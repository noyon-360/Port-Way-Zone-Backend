package main

import (
	"fmt"
	"sync"
)

type ConnectionManager struct {
	sessions map[string]*SSHRunner
	mu       sync.Mutex
}

func NewConnectionManager() *ConnectionManager {
	return &ConnectionManager{
		sessions: make(map[string]*SSHRunner),
	}
}

func (cm *ConnectionManager) GetExistingRunner(userID string) (*SSHRunner, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if existing, ok := cm.sessions[userID]; ok {
		return existing, nil
	}
	return nil, fmt.Errorf("no active session found for user %s. Please call /connect first", userID)
}

func (cm *ConnectionManager) GetRunner(userID, ip, user, pass string) (*SSHRunner, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// 1. Check if user already has an active session
	if existing, ok := cm.sessions[userID]; ok {
		// If it's a different IP, close the old one
		if existing.Config.IP != ip {
			fmt.Printf("🔄 User %s switching VPS from %s to %s. Closing old session.\n", userID, existing.Config.IP, ip)
			existing.Close()
			delete(cm.sessions, userID)
		} else {
			// Same IP, reuse existing session
			return existing, nil
		}
	}

	// 2. Create new session
	fmt.Printf("🔑 Creating new session for User %s on VPS %s\n", userID, ip)
	runner, err := NewSSHRunner(ip, user, pass)
	if err != nil {
		return nil, err
	}

	cm.sessions[userID] = runner
	return runner, nil
}

func (cm *ConnectionManager) CloseSession(userID string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if runner, ok := cm.sessions[userID]; ok {
		runner.Close()
		delete(cm.sessions, userID)
	}
}
