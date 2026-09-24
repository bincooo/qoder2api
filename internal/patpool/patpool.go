// Package patpool 维护一组由启用 PAT 构建的 COSY 会话，按 round-robin 轮换
// 提供给桥接层使用；认证失效时可移除对应会话并禁用其 PAT。
package patpool

import (
	"log"
	"sync"

	"qoder2api/internal/cosy"
	"qoder2api/internal/patstore"
)

// SessionBuilder 由 PAT 构建一个会话。测试可注入替身。
type SessionBuilder func(pat string) (*cosy.SessionContext, error)

// DefaultBuilder 是生产构建路径：ExchangeJobToken + NewSession，机器标识随机生成。
func DefaultBuilder(pat string) (*cosy.SessionContext, error) {
	mid := cosy.NewUUID()
	mtoken := base64WithoutPad([]byte((cosy.NewUUID() + cosy.NewUUID())[:50]))
	mtype := stripDashes(cosy.NewUUID())[:18]

	js, err := exchangeJobToken(pat, mid, mtoken, mtype)
	if err != nil {
		return nil, err
	}
	return cosy.NewSession(cosy.AuthIdentity{
		Name:               js.Name,
		Aid:                js.ID,
		Uid:                js.ID,
		UserType:           defaultUserType(js.UserType),
		SecurityOauthToken: js.SecurityOauthToken,
		RefreshToken:       js.RefreshToken,
	}, mid, mtoken, mtype)
}

type poolSession struct {
	id   int64
	sess *cosy.SessionContext
}

// Pool 持有全部有效会话，按 id 升序 round-robin 分发。
type Pool struct {
	mu       sync.RWMutex
	store    *patstore.Store
	build    SessionBuilder
	sessions []poolSession
	next     int
}

// NewPool 读取库中所有启用的 PAT 并构建会话池；单个 PAT 构建失败只记日志并跳过。
func NewPool(store *patstore.Store, build SessionBuilder) (*Pool, error) {
	p := &Pool{store: store, build: build}
	if err := p.Refresh(); err != nil {
		return nil, err
	}
	return p, nil
}

// Refresh 重新读取启用 PAT 并重建整个会话池。
func (p *Pool) Refresh() error {
	pats, err := p.store.ListEnabled()
	if err != nil {
		return err
	}
	var sessions []poolSession
	for _, pa := range pats {
		sess, err := p.build(pa.PAT)
		if err != nil {
			log.Printf("[pool] build session for pat id=%d failed: %v", pa.ID, err)
			continue
		}
		sessions = append(sessions, poolSession{id: pa.ID, sess: sess})
	}

	p.mu.Lock()
	p.sessions = sessions
	p.next = 0
	p.mu.Unlock()
	return nil
}

// Next 返回下一个会话及其 PAT id；池为空时返回 ok=false。
func (p *Pool) Next() (id int64, sess *cosy.SessionContext, ok bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.sessions) == 0 {
		return 0, nil, false
	}
	if p.next >= len(p.sessions) {
		p.next = 0
	}
	ps := p.sessions[p.next]
	p.next++
	return ps.id, ps.sess, true
}

// MarkDead 禁用指定 PAT 并将其会话移出池。
func (p *Pool) MarkDead(id int64) {
	if err := p.store.Disable(id); err != nil {
		log.Printf("[pool] disable pat id=%d failed: %v", id, err)
	}
	p.mu.Lock()
	kept := p.sessions[:0]
	for _, ps := range p.sessions {
		if ps.id != id {
			kept = append(kept, ps)
		}
	}
	p.sessions = kept
	if p.next > len(p.sessions) {
		p.next = 0
	}
	p.mu.Unlock()
	log.Printf("[pool] pat id=%d disabled and removed", id)
}

// Size 返回当前池中会话数。
func (p *Pool) Size() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.sessions)
}

// RecordCall 记录一次会话使用，累加对应 PAT 的调用次数。
func (p *Pool) RecordCall(id int64) error {
	return p.store.IncCall(id)
}
