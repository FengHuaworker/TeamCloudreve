package constants

// These values will be injected at build time, DO NOT EDIT.

// BackendVersion 当前后端版本号
var BackendVersion = "4.14.0"

// IsPro 是否为Pro版本
var IsPro = "false"

var IsProBool = IsPro == "true"

// LastCommit 最后commit id
var LastCommit = "000000"

const (
	APIPrefix      = "/api/v4"
	APIPrefixSlave = "/api/v4/slave"
	CrHeaderPrefix = "X-Cr-"
)

const CloudreveScheme = "cloudreve"

type (
	FileSystemType string
)

const (
	FileSystemMy           = FileSystemType("my")
	FileSystemShare        = FileSystemType("share")
	FileSystemTrash        = FileSystemType("trash")
	FileSystemSharedWithMe = FileSystemType("shared_with_me")
	// FileSystemTeam 团队空间（第 1 步：只读导航壳）。
	// 路径形如 cloudreve://team 与 cloudreve://team/<projectID>。
	FileSystemTeam    = FileSystemType("team")
	FileSystemUnknown = FileSystemType("unknown")
)
