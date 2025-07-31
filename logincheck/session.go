// Package main 实现了一个会话管理系统，用于处理用户会话的创建、验证和清理。
// 该系统支持会话的自动过期和定期清理功能，确保系统资源的有效利用。
package logincheck

import (
	"sync"
	"time"
	"log"
)

// Session 表示一个用户会话，包含用户标识和会话的时间信息
type Session struct {
	UserID    string    // 用户的唯一标识符
	CreatedAt time.Time // 会话创建时间
	ExpiresAt time.Time // 会话过期时间
}

// SessionManager 管理所有用户会话，提供会话的创建、验证和删除功能
type SessionManager struct {
	sessions map[string]*Session // 存储会话ID到会话对象的映射
	mutex    sync.RWMutex       // 用于保护会话map的读写锁
}

// GlobalSessionManager 是全局的会话管理器实例
var GlobalSessionManager *SessionManager

// init 初始化全局会话管理器并启动过期会话清理协程
func init() {
	GlobalSessionManager = NewSessionManager()
	go GlobalSessionManager.cleanupExpiredSessions()
}

// NewSessionManager 创建并返回一个新的会话管理器实例
func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*Session),
	}
}

// CreateSession 为指定用户创建一个新的会话
// userID: 用户的唯一标识符
// 返回值: 新创建的会话ID
func (sm *SessionManager) CreateSession(userID string,sessionID string) string {
	//sessionID := GenerateSessionID()
	session := &Session{
		UserID:    userID,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour), // 会话有效期为24小时
	}

	log.Println("创建新的Session")
	sm.mutex.Lock()
	sm.sessions[sessionID] = session
	sm.mutex.Unlock()

	return sessionID
}

// ValidateSession 验证会话是否有效
// sessionID: 要验证的会话ID
// 返回值: 如果会话有效返回true，否则返回false
func (sm *SessionManager) ValidateSession(sessionID string) bool {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	session, exists := sm.sessions[sessionID]
	if !exists {
		log.Println("Session不存在")
		return false
	}

	// 检查会话是否过期
	if time.Now().After(session.ExpiresAt) {
		delete(sm.sessions, sessionID)
		log.Println("Session已过期")
		return false
	}
	log.Println("Session有效") 
	return true
}

// RemoveSession 从会话管理器中移除指定的会话
// sessionID: 要移除的会话ID
func (sm *SessionManager) RemoveSession(sessionID string) {
	sm.mutex.Lock()
	delete(sm.sessions, sessionID)
	sm.mutex.Unlock()
}

// cleanupExpiredSessions 定期清理过期的会话
// 该方法在后台协程中运行，每小时检查一次过期会话
func (sm *SessionManager) cleanupExpiredSessions() {
	for {
		time.Sleep(1 * time.Hour) // 每小时执行一次清理
		sm.mutex.Lock()
		for id, session := range sm.sessions {
			if time.Now().After(session.ExpiresAt) {
				delete(sm.sessions, id)
			}
		}
		sm.mutex.Unlock()
	}
}