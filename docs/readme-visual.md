# README 配图

README 使用本地资源，提交源码时一同提交 `assets/readme/`。所有截图均使用合成演示数据，不包含个人账号。

| 文件 | 来源 |
| --- | --- |
| `assets/readme/overview.png` | 3.0 主窗口“用量总览”实机截图（浅色，含智能体分布） |
| `assets/readme/quota.png` | “账户额度”实机截图：Codex 与 Claude Code 分组 |
| `assets/readme/activity.png` | “活动”页面截图（浅色） |
| `assets/readme/history.png` | “历史分析”页面截图（深色） |
| `assets/readme/widget.png` | Go / Win32 原生透明桌面小窗口截图 |
| `assets/readme/cover.png` | 与总览截图相同 |
| `assets/readme/charts.png` | 平滑曲线说明图，见 [曲线平滑](chart-smoothing.md) |

3.0 的截图来自 `npm run native:check` 的真实窗口验收（`artifacts/native-acceptance/`），数据来自 `internal/testfixture`，其中 Claude Code 数据同样为合成数据。此前版本的页面截图由 MyGO 的无头测试渲染器 `ui.NewTester` 以 1280×800 DIP、1.5 倍缩放、减少动态效果的设置绘制，数据来自 `internal/testfixture`。无头渲染没有 Windows Mica 材质，侧栏显示为不透明的系统侧栏色；真实窗口中侧栏会透出桌面颜色。

封面以 HTML 排版两张截图后由 Chromium 截图生成，不重绘界面内容。
