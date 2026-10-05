import { ExpandMoreRounded } from "@mui/icons-material";
import { Accordion, styled } from "@mui/material";
import MuiAccordionSummary, { AccordionSummaryProps } from "@mui/material/AccordionSummary";

// 注意：本模块同时是共享样式模块，AccordionSummary / StyledAccordion
// 被 FileViewerList、EmailTemplates、Extractors、Generators 复用，不可删除。

export const AccordionSummary = styled((props: AccordionSummaryProps) => <MuiAccordionSummary {...props} />)(
  ({ theme }) => ({
    fontSize: theme.typography.body2.fontSize,
    paddingLeft: theme.spacing(4),
    "& .MuiFormControlLabel-label": {
      fontSize: theme.typography.body2.fontSize,
    },
    "& .MuiCheckbox-root": {
      marginRight: theme.spacing(2),
    },
  }),
);

export const StyledAccordion = styled(Accordion)(({ theme }) => ({
  boxShadow: "none",
  border: `1px solid ${theme.palette.divider}`,
  "&::before": {
    display: "none",
  },
}));

export interface SettingSectionProps {}

// 第三方登录（QQ 互联 / Logto / OIDC）在社区版为 Pro 独占功能，
// 本项目明确不使用任何第三方认证，故此组件不再渲染任何内容。
const SSOSettings = () => {
  return null;
};

export default SSOSettings;
