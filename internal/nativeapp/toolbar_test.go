package nativeapp

import (
	"fmt"
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestToolbarAlignsWithTrailingFilter(t *testing.T) {
	for _, width := range []int{980, 1380, 1800} {
		for _, english := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/english=%t", width, english), func(t *testing.T) {
				a := fixtureApp(t)
				if english {
					obj(a.data["settings"])["language"] = "en"
				}
				tt := ui.NewTester(a.View, width, 960)
				tt.SetPreferences(ui.Preferences{ReduceMotion: true})
				tt.SetTitleBar(ui.TitleBar{Height: 32, Right: 138})
				export, ok := tt.Find(a.tr("导出 CSV"))
				project, found := tt.Find(a.tr("全部项目"))
				if !ok || !found || math.Abs(float64(export.X+export.W-project.X-project.W)) > 1 {
					t.Fatalf("export %+v and project %+v are not right aligned", export, project)
				}
				refresh, _ := tt.Find(a.tr("刷新数据与额度"))
				if refresh.Y < 32 || export.Y < 32 || refresh.X+refresh.W > export.X {
					t.Fatal("toolbar actions overlap each other or the native caption")
				}
			})
		}
	}
}
