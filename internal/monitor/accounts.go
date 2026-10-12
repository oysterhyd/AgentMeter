package monitor

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

func readObject(file string) Object {
	b, e := os.ReadFile(file)
	if e != nil {
		return Object{}
	}
	var v Object
	_ = json.Unmarshal(b, &v)
	return obj(v)
}
func claims(token string) Object {
	_, payload, ok := strings.Cut(token, ".")
	if !ok {
		return Object{}
	}
	payload, _, _ = strings.Cut(payload, ".")
	b, e := base64.RawURLEncoding.DecodeString(payload)
	if e != nil {
		return Object{}
	}
	var v Object
	_ = json.Unmarshal(b, &v)
	return obj(v)
}
func ReadAccount(home string) Object {
	auth := readObject(filepath.Join(home, "auth.json"))
	tokens := obj(auth["tokens"])
	raw, ok := tokens["account_id"].(string)
	if !ok || strings.TrimSpace(raw) == "" {
		return nil
	}
	c := claims(text(tokens["id_token"]))
	email := text(c["email"])
	user := text(obj(c["https://api.openai.com/auth"])["chatgpt_user_id"])
	if user == "" {
		user = text(c["sub"])
	}
	if user == "" {
		user = email
	}
	id := hash("chatgpt:" + raw + ":" + user)
	label := email
	if len(email) >= 200 {
		label = "Account " + id[:8]
	}
	return Object{"id": id, "label": label}
}
func piOpenAIAuth(home string) bool {
	auth := obj(readObject(filepath.Join(home, "auth.json"))["openai"])
	if auth["type"] != "oauth" {
		return false
	}
	token, ok := auth["access"].(string)
	if !ok {
		return false
	}
	c := claims(token)
	direct := "chatgpt.tokens.use.direct"
	has := func(v any, want string) bool {
		if text(v) == want {
			return true
		}
		if list, ok := v.([]any); ok {
			for _, x := range list {
				if x == want {
					return true
				}
			}
		}
		return false
	}
	if c["iss"] != "https://auth.openai.com" || !has(c["aud"], "https://api.openai.com/v1") || !slices.Contains(strings.Fields(text(c["scope"])), direct) || !has(auth["scopes"], direct) {
		return false
	}
	file := filepath.Join(home, "models.json")
	data, e := os.ReadFile(file)
	if e != nil && !os.IsNotExist(e) {
		return false
	}
	config := Object{}
	if e == nil {
		if json.Unmarshal(data, &config) != nil {
			return false
		}
	}
	override := obj(obj(config["providers"])["openai"])
	if truth(override["apiKey"]) || truth(override["headers"]) || (truth(override["api"]) && override["api"] != "openai-responses") {
		return false
	}
	if truth(override["baseUrl"]) && strings.TrimSuffix(text(override["baseUrl"]), "/") != "https://api.openai.com/v1" {
		return false
	}
	if models, ok := override["models"].([]any); ok {
		for _, v := range models {
			m := obj(v)
			if truth(m["apiKey"]) || truth(m["headers"]) || truth(m["baseUrl"]) || (truth(m["api"]) && m["api"] != "openai-responses") {
				return false
			}
		}
	}
	return true
}
func readPiAccount(home, provider string) Object {
	if provider == "openai" && !piOpenAIAuth(home) {
		return nil
	}
	a := obj(readObject(filepath.Join(home, "auth.json"))[provider])
	if a["type"] != "oauth" {
		return nil
	}
	token, ok := a["access"].(string)
	if !ok {
		return nil
	}
	c := claims(token)
	auth := obj(c["https://api.openai.com/auth"])
	raw, user := text(auth["chatgpt_account_id"]), text(auth["chatgpt_user_id"])
	if user == "" {
		user = text(c["sub"])
	}
	if raw == "" || user == "" {
		return nil
	}
	id := hash("chatgpt:" + raw + ":" + user)
	emailValue := c["email"]
	if !truth(emailValue) {
		emailValue = obj(c["https://api.openai.com/profile"])["email"]
	}
	email, ok := emailValue.(string)
	if !ok || len(email) >= 200 {
		email = "Account " + id[:8]
	}
	return Object{"id": id, "label": email}
}
func (s *Store) observe(home, now, source string) error {
	a := ReadAccount(home)
	if source == "pi" {
		a = readPiAccount(home, "openai-codex")
	}
	if source == "pi-openai" {
		a = readPiAccount(home, "openai")
	}
	id := Unknown
	if a != nil {
		id = text(a["id"])
		if e := s.exec("INSERT INTO accounts VALUES(?,?,?) ON CONFLICT(id) DO NOTHING", id, a["label"], "detected"); e != nil {
			return e
		}
	}
	prev := s.mustOne("SELECT * FROM account_observations WHERE source=? ORDER BY id DESC LIMIT 1", source)
	var e error
	if prev["account"] == id && now >= text(prev["ended"]) {
		e = s.exec("UPDATE account_observations SET ended=? WHERE id=?", now, prev["id"])
	} else {
		e = s.exec("INSERT INTO account_observations(account,started,ended,source) VALUES(?,?,?,?)", id, now, now, source)
	}
	s.intervals = nil
	if e != nil {
		return e
	}
	if source == "codex" {
		return s.set("currentAccount", id)
	}
	return nil
}
func (s *Store) accountAt(ts, source string) string {
	if s.intervals == nil {
		s.intervals = s.mustQuery("SELECT account,started,ended,source FROM account_observations ORDER BY id DESC")
	}
	for _, r := range s.intervals {
		if r["source"] == source && text(r["started"]) <= ts && text(r["ended"]) >= ts {
			if truth(r["account"]) {
				return text(r["account"])
			}
			return Unknown
		}
	}
	return Unknown
}
