package worktree

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// fakeTmux 装一个假 tmux：list-panes 的输出从 panesFile 读，测试中途改文件即可
// 模拟「用户在会话里 cd 走了」。返回 panesFile 路径。
func fakeTmux(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	panesFile := filepath.Join(dir, "panes.txt")
	bin := filepath.Join(dir, "tmux")
	script := fmt.Sprintf("#!/bin/sh\ncat %q\n", panesFile)
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMUX_BIN", bin)
	return panesFile
}

func setPanes(t *testing.T, panesFile, content string) {
	t.Helper()
	if err := os.WriteFile(panesFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// 端到端语义：会话在仓库里建起来 → cd 去 /tmp → 归属仍是原仓库（annotation.primary）。
func TestAnnotationsStickToHomeAfterCd(t *testing.T) {
	ctx := context.Background()
	repo := mkRepo(t)
	panesFile := fakeTmux(t)
	s := New(t.TempDir())

	setPanes(t, panesFile, "work\t1\t"+repo+"\n")
	if got := s.Annotations(ctx)["work"]; got == nil || got.Primary == nil || got.Primary.Repo != canonical(repo) {
		t.Fatalf("建会话时归属应为 %s，实得 %+v", repo, got)
	}

	// 用户 cd 到仓库外（连 git 仓库都不是）——归属不许跟着走
	away := t.TempDir()
	setPanes(t, panesFile, "work\t1\t"+away+"\n")
	ann := s.Annotations(ctx)["work"]
	if ann == nil || ann.Primary == nil || ann.Primary.Repo != canonical(repo) {
		t.Fatalf("cd 走之后归属漂了: %+v", ann)
	}
	if ann.Home != canonical(repo) {
		t.Fatalf("home = %q, want %q", ann.Home, canonical(repo))
	}
	if cwds := s.SessionCwds(ctx)["work"]; len(cwds) != 1 || cwds[0] != canonical(repo) {
		t.Fatalf("SessionCwds = %v, want [%s]", cwds, canonical(repo))
	}

	// 会话没了 → 归属收敛，不留残行
	setPanes(t, panesFile, "")
	setPanes(t, panesFile, "other\t1\t"+away+"\n")
	_ = s.Annotations(ctx)
	if h := s.SessionHome("work"); h != "" {
		t.Fatalf("死会话残行未清: %q", h)
	}
}

// 归属钉死的核心语义：第一次见到就定下来，之后 cwd 怎么变都不改（用户在终端里
// cd 出去，不等于换项目）。
func TestHomePinnedAgainstCwdDrift(t *testing.T) {
	h := newHomeStore("")
	if got := h.pin("s1", "/repo/a"); got != "/repo/a" {
		t.Fatalf("first pin = %q, want /repo/a", got)
	}
	if got := h.pin("s1", "/tmp/elsewhere"); got != "/repo/a" {
		t.Fatalf("cwd 漂移后 home = %q, want /repo/a", got)
	}
	if got := h.get("s1"); got != "/repo/a" {
		t.Fatalf("get = %q, want /repo/a", got)
	}
}

// 显式绑定（建会话时就知道目录）覆盖已钉死的值，且不会被后续采样改回去。
func TestBindOverridesPin(t *testing.T) {
	h := newHomeStore("")
	h.pin("s1", "/repo/a")
	h.bind("s1", "/repo/a/.worktrees/task")
	if got := h.pin("s1", "/repo/a"); got != "/repo/a/.worktrees/task" {
		t.Fatalf("home = %q, want worktree 路径", got)
	}
}

func TestRenameAndForget(t *testing.T) {
	h := newHomeStore("")
	h.bind("old", "/repo/a")
	h.rename("old", "new")
	if h.get("old") != "" || h.get("new") != "/repo/a" {
		t.Fatalf("rename 后 old=%q new=%q", h.get("old"), h.get("new"))
	}
	h.forget("new")
	if h.get("new") != "" {
		t.Fatalf("forget 后仍有归属: %q", h.get("new"))
	}
}

// reconcile 只清死会话；tmux 读失败(alive 空)时一行都不能动。
func TestReconcileKeepsAliveAndSkipsEmpty(t *testing.T) {
	h := newHomeStore("")
	h.bind("alive", "/repo/a")
	h.bind("dead", "/repo/b")
	h.reconcile(nil)
	if h.get("dead") == "" {
		t.Fatal("alive 集合为空时不应删任何行")
	}
	h.reconcile(map[string]bool{"alive": true})
	if h.get("alive") != "/repo/a" {
		t.Fatal("活会话归属被误删")
	}
	if h.get("dead") != "" {
		t.Fatal("死会话残行未收敛")
	}
}

// 落盘 + 重建：后端重启不丢归属。
func TestPersistAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	h := newHomeStore(dir)
	h.bind("s1", "/repo/a")
	b, err := os.ReadFile(filepath.Join(dir, "session-homes.json"))
	if err != nil {
		t.Fatalf("未落盘: %v", err)
	}
	var m map[string]string
	if err := json.Unmarshal(b, &m); err != nil || m["s1"] != "/repo/a" {
		t.Fatalf("落盘内容 = %s (%v)", b, err)
	}
	if got := newHomeStore(dir).get("s1"); got != "/repo/a" {
		t.Fatalf("重启后 home = %q, want /repo/a", got)
	}
}

// homePanes：每会话一条、Active=true，按会话名稳定排序（供 join 复用老的最长前缀逻辑）。
func TestHomePanes(t *testing.T) {
	ps := homePanes(map[string]string{"b": "/repo/b", "a": "/repo/a"})
	if len(ps) != 2 || ps[0].Session != "a" || ps[1].Session != "b" {
		t.Fatalf("panes = %+v", ps)
	}
	for _, p := range ps {
		if !p.Active {
			t.Fatalf("home pane 应为 active: %+v", p)
		}
	}
}
