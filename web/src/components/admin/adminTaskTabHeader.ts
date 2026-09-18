// AdminTaskTabStat 定义任务面板标签栏的统计项数据结构（来自Trae）
export type AdminTaskTabStat = {
  icon: string;
  value: string | number;
  label: string;
  tone?: "blue" | "red" | "purple" | "amber";
  refresh?: boolean;
};
