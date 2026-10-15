# README 配图

README 使用本地资源，提交源码时一同提交 `assets/readme/`。所有截图均使用合成演示数据，不包含个人账号。

| 文件 | 来源 |
| --- | --- |
| `assets/readme/overview.png` | 2.9 主窗口“用量总览”的原生绘制截图（浅色） |
| `assets/readme/activity.png` | “活动”页面截图（浅色） |
| `assets/readme/history.png` | “历史分析”页面截图（深色） |
| `assets/readme/widget.png` | Go / Win32 原生透明桌面小窗口截图 |
| `assets/readme/cover.png` | 用总览截图与小窗口截图直接排版的封面，只增加背景、圆角和阴影 |
| `assets/readme/charts.png` | 平滑曲线说明图，见 [曲线平滑](chart-smoothing.md) |

页面截图由 MyGO 的无头测试渲染器 `ui.NewTester` 以 1280×800 DIP、1.5 倍缩放、减少动态效果的设置绘制，数据来自 `internal/testfixture`。无头渲染没有 Windows Mica 材质，侧栏显示为不透明的系统侧栏色；真实窗口中侧栏会透出桌面颜色。

封面以 HTML 排版两张截图后由 Chromium 截图生成，不重绘界面内容。
