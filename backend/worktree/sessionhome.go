// 会话「归属目录」钉死（session home）。
//
// 以前 session → worktree/项目 的归属全靠**实时** pane cwd 现算：会话在项目里
// 建好，用户在终端里 `cd` 去别处（翻日志、看 /tmp、去另一个仓库瞄一眼），归属
// 立刻跟着漂——项目会话数掉 0、详情页任务流空掉、甚至被别的项目抢走。可 cd 是
// 终端里的日常动作，不是「换项目」。
//
// 现在：会话创建时（Web 建会话/建 worktree 会话/竞赛/fork 都知道目录）就把归属
// 目录钉死；CLI 直建的会话则第一次被看见时钉死。之后一律只认这个 home 目录，
// pane cwd 怎么漂都不改归属。分支/worktree 状态仍按 home 目录现算，所以在 home
// 里切分支照样实时反映。
//
// 钉死关系落 <dataDir>/session-homes.json（后端重启不丢），会话消失即收敛；
// 文件丢了也只是回到「按当前 cwd 重新钉一次」，不影响任何真相源。
package worktree

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

type homeStore struct {
	mu   sync.Mutex
	path string            // 空 = 只在内存里（测试）
	m    map[string]string // session → canonical 归属目录
}

func newHomeStore(dataDir string) *homeStore {
	h := &homeStore{m: map[string]string{}}
	if dataDir == "" {
		return h
	}
	h.path = filepath.Join(dataDir, "session-homes.json")
	if b, err := os.ReadFile(h.path); err == nil {
		_ = json.Unmarshal(b, &h.m)
	}
	return h
}

// save 落盘（调用方须持锁）：tmp + rename 原子替换。写失败只丢钉死关系，下次重钉。
func (h *homeStore) save() {
	if h.path == "" {
		return
	}
	b, err := json.MarshalIndent(h.m, "", "  ")
	if err != nil {
		return
	}
	tmp := h.path + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, h.path)
	}
}

// pin 首次见到即钉死并返回 home；已钉死则原样返回（cwd 漂移不改归属）。
func (h *homeStore) pin(session, cwd string) string {
	if session == "" {
		return ""
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if d := h.m[session]; d != "" {
		return d
	}
	if cwd == "" {
		return ""
	}
	h.m[session] = cwd
	h.save()
	return cwd
}

// bind 显式绑定（创建会话时就知道它属于哪个目录）：覆盖旧值，且早于任何 pane 采样，
// 免得「建好 5s 内就 cd 走」把归属钉错。
func (h *homeStore) bind(session, dir string) {
	if session == "" || dir == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.m[session] == dir {
		return
	}
	h.m[session] = dir
	h.save()
}

func (h *homeStore) get(session string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.m[session]
}

func (h *homeStore) rename(old, neu string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	d := h.m[old]
	if d == "" {
		return
	}
	delete(h.m, old)
	h.m[neu] = d
	h.save()
}

func (h *homeStore) forget(session string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.m[session]; !ok {
		return
	}
	delete(h.m, session)
	h.save()
}

// reconcile 收敛残行：alive 之外的会话删除（裸 tmux kill-session 绕过 API 也能清干净）。
// alive 为空（tmux 读失败）时不动——宁可留残行，也不能把活会话的归属抹掉。
func (h *homeStore) reconcile(alive map[string]bool) {
	if len(alive) == 0 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	changed := false
	for s := range h.m {
		if !alive[s] {
			delete(h.m, s)
			changed = true
		}
	}
	if changed {
		h.save()
	}
}

// homePanes 把 {session → home} 拍回 pane 列表，让既有的「按 cwd 最长前缀归属」
// 逻辑原样复用：每会话一条、Active=true（home 就是它的归属，无歧义）。
func homePanes(homes map[string]string) []pane {
	out := make([]pane, 0, len(homes))
	for sess, dir := range homes {
		out = append(out, pane{Session: sess, Active: true, Cwd: dir})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Session < out[j].Session })
	return out
}

// ── Service 外部接口 ──────────────────────────────────────

// BindSessionHome 显式钉死会话归属目录（建会话的编排方调用）。
func (s *Service) BindSessionHome(session, dir string) {
	if dir == "" {
		return
	}
	s.homes.bind(session, canonical(dir))
}

// SessionHome 返回会话钉死的归属目录（未钉死返回空串）。
func (s *Service) SessionHome(session string) string { return s.homes.get(session) }

// RenameSessionHome 会话改名时迁移钉死关系。
func (s *Service) RenameSessionHome(old, neu string) { s.homes.rename(old, neu) }

// ForgetSessionHome 会话被杀时清掉钉死关系（名称复用不继承旧归属）。
func (s *Service) ForgetSessionHome(session string) { s.homes.forget(session) }

// sessionHomes 返回 {session → home 目录}：已钉死的照旧，没钉过的按当前 pane
// （活动 pane 优先）钉一次；顺带收敛死会话残行。这是 session 归属的唯一入口，
// Annotations / SessionCwds / worktree 会话 join 全部走它。
func (s *Service) sessionHomes(ctx context.Context) map[string]string {
	panes := tmuxPanes(ctx)
	alive := make(map[string]bool, len(panes))
	first := map[string]string{}
	active := map[string]string{}
	for _, p := range panes {
		alive[p.Session] = true
		if _, ok := first[p.Session]; !ok {
			first[p.Session] = p.Cwd
		}
		if p.Active {
			active[p.Session] = p.Cwd
		}
	}
	s.homes.reconcile(alive)
	out := make(map[string]string, len(alive))
	for sess := range alive {
		cwd := active[sess]
		if cwd == "" {
			cwd = first[sess]
		}
		if home := s.homes.pin(sess, cwd); home != "" {
			out[sess] = home
		}
	}
	return out
}
