package nativeapp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Run only in the isolated native acceptance profile. Scope the app to each
// agent through the toolbar's path, check every row in the snapshot belongs
// to it, and capture the scoped overview and quota pages.
func (a *App) smokeAgents() {
	var savedFilter Object
	a.updateWait(func() { savedFilter = clone(a.filter); a.filter["range"] = "all"; a.navigate(0) })
	wait := func() bool {
		deadline := time.Now().Add(12 * time.Second)
		for time.Now().Before(deadline) {
			ready := false
			a.updateWait(func() {
				key, _ := json.Marshal(a.requestFilter())
				ready = !a.loading && a.snapshotPage == a.page && a.snapshotKey == string(key)
			})
			if ready {
				return true
			}
			time.Sleep(5 * time.Millisecond)
		}
		return false
	}
	verified := wait()
	results := []Object{}
	capture := func(name string) {
		time.Sleep(motionReveal + 60*time.Millisecond)
		if png, err := a.win.CapturePage(); err == nil {
			_ = os.WriteFile(filepath.Join(a.options.Capture, name), png, 0600)
		}
	}
	for _, agent := range []string{agentClaude, agentCodex, ""} {
		a.updateWait(func() { a.navigate(0); a.setAgent(agent) })
		ready := wait()
		scoped := ready
		a.updateWait(func() {
			for _, row := range objects(a.data["agents"]) {
				if agent != "" && str(row["name"]) != agent {
					scoped = false
				}
			}
			for _, q := range objects(a.data["quotas"]) {
				if agent != "" && quotaAgent(q) != agent {
					scoped = false
				}
			}
			for _, model := range stringsOf(obj(a.data["options"])["models"]) {
				if agent != "" && claudeModel(model) != (agent == agentClaude) {
					scoped = false
				}
			}
			results = append(results, Object{"agent": agent, "ready": ready, "scoped": scoped, "requests": obj(a.data["sums"])["requests"]})
		})
		verified = verified && scoped
		if agent == agentClaude {
			capture("overview-claude.png")
			a.updateWait(func() { a.navigate(3) })
			verified = wait() && verified
			capture("quota-claude.png")
		}
	}
	// With both agents in scope, the overview compares them.
	capture("overview-agents.png")
	a.updateWait(func() {
		verified = verified && len(stringsOf(a.data["agentsPresent"])) == 2 && a.errorText == ""
		data, _ := json.MarshalIndent(Object{"verified": verified, "agents": results}, "", "  ")
		_ = os.WriteFile(filepath.Join(a.options.Capture, "agents-result.json"), data, 0600)
		if !verified {
			a.errorText = "智能体切换验收失败"
		}
		a.filter = savedFilter
		a.generation++
		a.navigate(4)
	})
}
