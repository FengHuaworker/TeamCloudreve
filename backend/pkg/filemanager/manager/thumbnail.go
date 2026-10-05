package manager

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"runtime"
	"time"

	"github.com/cloudreve/Cloudreve/v4/ent"
	"github.com/cloudreve/Cloudreve/v4/ent/task"
	"github.com/cloudreve/Cloudreve/v4/inventory/types"
	"github.com/cloudreve/Cloudreve/v4/pkg/filemanager/driver/local"
	"github.com/cloudreve/Cloudreve/v4/pkg/filemanager/fs"
	"github.com/cloudreve/Cloudreve/v4/pkg/filemanager/fs/dbfs"
	"github.com/cloudreve/Cloudreve/v4/pkg/filemanager/manager/entitysource"
	"github.com/cloudreve/Cloudreve/v4/pkg/logging"
	"github.com/cloudreve/Cloudreve/v4/pkg/queue"
	"github.com/cloudreve/Cloudreve/v4/pkg/thumb"
	"github.com/cloudreve/Cloudreve/v4/pkg/util"
	"github.com/samber/lo"
)

// Thumbnail returns the thumbnail entity of the file.
func (m *manager) Thumbnail(ctx context.Context, uri *fs.URI) (entitysource.EntitySource, error) {
	// retrieve file info
	file, err := m.fs.Get(ctx, uri, dbfs.WithFileEntities(), dbfs.WithFilePublicMetadata())
	if err != nil {
		return nil, fmt.Errorf("failed to get file: %w", err)
	}

	// 0. Check if thumb is disabled in this file.
	if _, ok := file.Metadata()[dbfs.ThumbDisabledKey]; ok || file.Type() != types.FileTypeFile {
		return nil, fs.ErrEntityNotExist
	}

	// 1. If thumbnail entity exist, use it.
	entities := file.Entities()
	thumbEntity, found := lo.Find(entities, func(e fs.Entity) bool {
		return e.Type() == types.EntityTypeThumbnail
	})
	if found {
		thumbSource, err := m.GetEntitySource(ctx, 0, fs.WithEntity(thumbEntity))
		if err != nil {
			return nil, fmt.Errorf("failed to get entity source: %w", err)
		}

		thumbSource.Apply(entitysource.WithDisplayName(file.DisplayName() + ".jpg"))
		return thumbSource, nil
	}

	latest := file.PrimaryEntity()
	// If primary entity not exist, or it's empty
	if latest == nil || latest.ID() == 0 {
		return nil, fmt.Errorf("failed to get latest version")
	}

	// 2. Thumb entity not exist, try native policy generator
	_, handler, err := m.getEntityPolicyDriver(ctx, latest, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get entity policy driver: %w", err)
	}
	capabilities := handler.Capabilities()
	// Check if file extension and size is supported by native policy generator.
	if capabilities.ThumbSupportAllExts || util.IsInExtensionList(capabilities.ThumbSupportedExts, file.DisplayName()) &&
		(capabilities.ThumbMaxSize == 0 || latest.Size() <= capabilities.ThumbMaxSize) &&
		!latest.Encrypted() {
		thumbSource, err := m.GetEntitySource(ctx, 0, fs.WithEntity(latest), fs.WithUseThumb(true))
		if err != nil {
			return nil, fmt.Errorf("failed to get latest entity source: %w", err)
		}

		thumbSource.Apply(entitysource.WithDisplayName(file.DisplayName()))
		return thumbSource, nil
	} else if capabilities.ThumbProxy {
		if err := m.fs.CheckCapability(ctx, uri,
			dbfs.WithRequiredCapabilities(dbfs.NavigatorCapabilityGenerateThumb)); err != nil {
			// Current FS does not support generate new thumb.
			return nil, fs.ErrEntityNotExist
		}

		thumbEntity, err := m.SubmitAndAwaitThumbnailTask(ctx, uri, file.Ext(), latest)
		if err != nil {
			return nil, fmt.Errorf("failed to execute thumb task: %w", err)
		}

		thumbSource, err := m.GetEntitySource(ctx, 0, fs.WithEntity(thumbEntity))
		if err != nil {
			return nil, fmt.Errorf("failed to get entity source: %w", err)
		}

		return thumbSource, nil
	} else {
		// 4. If proxy generator not support, mark thumb as not available.
		_ = disableThumb(ctx, m, uri)
	}

	return nil, fs.ErrEntityNotExist
}

func (m *manager) SubmitAndAwaitThumbnailTask(ctx context.Context, uri *fs.URI, ext string, entity fs.Entity) (fs.Entity, error) {
	es, err := m.GetEntitySource(ctx, 0, fs.WithEntity(entity))
	if err != nil {
		return nil, fmt.Errorf("failed to get entity source: %w", err)
	}

	defer es.Close()
	t := newGenerateThumbTask(ctx, m, uri, ext, es)
	if err := m.dep.ThumbQueue(ctx).QueueTask(ctx, t); err != nil {
		return nil, fmt.Errorf("failed to queue task: %w", err)
	}

	// Wait for task to finish
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-t.sig:
		if res.err != nil {
			return nil, fmt.Errorf("failed to generate thumb: %w", res.err)
		}

		return res.thumbEntity, nil
	}

}

func (m *manager) generateThumb(ctx context.Context, uri *fs.URI, ext string, es entitysource.EntitySource) (fs.Entity, error) {
	// Generate thumb
	pipeline := m.dep.ThumbPipeline()
	res, err := pipeline.Generate(ctx, es, ext, nil)
	if err != nil {
		if res != nil && res.Path != "" {
			_ = os.Remove(res.Path)
		}

		if !errors.Is(err, context.Canceled) && !m.stateless {
			if err := disableThumb(ctx, m, uri); err != nil {
				m.l.Warning("Failed to disable thumb: %v", err)
			}
		}

		return nil, fmt.Errorf("failed to generate thumb: %w", err)
	}

	defer os.Remove(res.Path)

	// Upload thumb entity
	thumbFile, err := os.Open(res.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to open temp thumb %q: %w", res.Path, err)
	}

	defer thumbFile.Close()
	fileInfo, err := thumbFile.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat temp thumb %q: %w", res.Path, err)
	}

	var (
		thumbEntity fs.Entity
	)
	if m.stateless {
		_, d, err := m.getEntityPolicyDriver(ctx, es.Entity(), nil)
		if err != nil {
			return nil, fmt.Errorf("failed to get storage driver: %w", err)
		}

		savePath := es.Entity().Source() + m.settings.ThumbSlaveSidecarSuffix(ctx)
		if err := d.Put(ctx, &fs.UploadRequest{
			File:   thumbFile,
			Seeker: thumbFile,
			Props:  &fs.UploadProps{SavePath: savePath},
		}); err != nil {
			return nil, fmt.Errorf("failed to save thumb sidecar: %w", err)
		}

		thumbEntity, err = local.NewLocalFileEntity(types.EntityTypeThumbnail, savePath)
		if err != nil {
			return nil, fmt.Errorf("failed to create local thumb entity: %w", err)
		}
	} else {
		entityType := types.EntityTypeThumbnail
		req := &fs.UploadRequest{
			Props: &fs.UploadProps{
				Uri:        uri,
				Size:       fileInfo.Size(),
				SavePath:   path.Clean(util.ReplaceMagicVar(m.settings.ThumbEntitySuffix(ctx), fs.Separator, true, true, time.Now(), m.user.ID, uri.Name(), uri.Path(), es.Entity().Source())),
				MimeType:   m.dep.MimeDetector(ctx).TypeByName("thumb.jpg"),
				EntityType: &entityType,
			},
			File:   thumbFile,
			Seeker: thumbFile,
		}

		// Generating thumb can be triggered by users with read-only permission.
		// We can bypass update permission check.
		//
		// 【2026-10-04 第 3 步】此处仍保留所有者旁路，但 ACL 不再被它跳过：
		//   · 本旁路现在只豁免「所有者校验」（acl.go 中跳过 ACL 的那段已删除）；
		//   · 本路径的 ACL 判定在 dbfs/upload.go 的 PrepareUpload 中，
		//     已按 thumbOnly 白名单豁免（EntityType == Thumbnail），
		//     即「写派生 entity」不参与面向用户的 ACL 写判定。
		// 两条合起来：只读用户仍能生成缩略图，且【不能】借此改写文件内容或元数据。
		ctx = dbfs.WithBypassOwnerCheck(ctx)

		file, err := m.Update(ctx, req, fs.WithEntityType(types.EntityTypeThumbnail))
		if err != nil {
			return nil, fmt.Errorf("failed to upload thumb entity: %w", err)
		}

		entities := file.Entities()
		found := false
		thumbEntity, found = lo.Find(entities, func(e fs.Entity) bool {
			return e.Type() == types.EntityTypeThumbnail
		})
		if !found {
			return nil, fmt.Errorf("failed to find thumb entity")
		}

	}

	if m.settings.ThumbGCAfterGen(ctx) {
		m.l.Debug("GC after thumb generation")
		runtime.GC()
	}

	return thumbEntity, nil
}

type (
	GenerateThumbTask struct {
		*queue.InMemoryTask
		es  entitysource.EntitySource
		ext string
		m   *manager
		uri *fs.URI
		sig chan *generateRes
	}
	generateRes struct {
		thumbEntity fs.Entity
		err         error
	}
)

func newGenerateThumbTask(ctx context.Context, m *manager, uri *fs.URI, ext string, es entitysource.EntitySource) *GenerateThumbTask {
	t := &GenerateThumbTask{
		InMemoryTask: &queue.InMemoryTask{
			DBTask: &queue.DBTask{
				Task: &ent.Task{
					CorrelationID: logging.CorrelationID(ctx),
					PublicState:   &types.TaskPublicState{},
				},
			},
		},
		es:  es,
		ext: ext,
		m:   m,
		uri: uri,
		sig: make(chan *generateRes, 2),
	}

	t.InMemoryTask.DBTask.Task.SetUser(m.user)
	return t
}

func (m *GenerateThumbTask) Do(ctx context.Context) (task.Status, error) {
	// Make sure user does not cancel request before we start generating thumb.
	select {
	case <-ctx.Done():
		err := ctx.Err()
		return task.StatusError, err
	default:
	}

	res, err := m.m.generateThumb(ctx, m.uri, m.ext, m.es)
	if err != nil {
		if errors.Is(err, thumb.ErrNotAvailable) {
			m.sig <- &generateRes{nil, err}
			return task.StatusCompleted, nil
		}

		return task.StatusError, err
	}

	m.sig <- &generateRes{res, nil}
	return task.StatusCompleted, nil
}

func (m *GenerateThumbTask) OnError(err error, d time.Duration) {
	m.InMemoryTask.OnError(err, d)
	m.sig <- &generateRes{nil, err}
}

func disableThumb(ctx context.Context, m *manager, uri *fs.URI) error {
	// 【2026-10-04 第 3 步】改用窄入口，不再经由 PatchMetadata + 所有者旁路。
	//
	// 原实现：
	//     m.fs.PatchMetadata(dbfs.WithBypassOwnerCheck(ctx), []*fs.URI{uri},
	//         fs.MetadataPatch{Key: dbfs.ThumbDisabledKey, ...})
	// 问题：PatchMetadata 的 key/value 由调用方任意控制，是用户语义的「改元数据」，
	// 必须受文件 ACL 约束；为放行缩略图而给它开旁路，
	// 等于让只读用户可以改写【任意】元数据。方向错误。
	//
	// 现改为只写 ThumbDisabled 这一个固定 key 的专用入口（dbfs.SetThumbDisabled），
	// 天然不可滥用；与 manage.go:248 清除该标记的既有做法同层。
	return m.fs.SetThumbDisabled(ctx, uri, true)
}
