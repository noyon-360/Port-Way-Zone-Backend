package main

import (
	"fmt"
	"sync"
)

type DeploymentTask struct {
	UserID string
	Config map[string]string
	Runner *SSHRunner
}

type QueueManager struct {
	taskChan   chan DeploymentTask
	statuses   map[string]string // userID -> status
	mu         sync.RWMutex
	maxWorkers int
}

func NewQueueManager(bufferSize, workers int) *QueueManager {
	qm := &QueueManager{
		taskChan:   make(chan DeploymentTask, bufferSize),
		statuses:   make(map[string]string),
		maxWorkers: workers,
	}

	// Start workers
	for i := 0; i < workers; i++ {
		go qm.worker(i)
	}

	return qm
}

func (qm *QueueManager) worker(id int) {
	fmt.Printf("👷 Worker %d started\n", id)
	for task := range qm.taskChan {
		qm.updateStatus(task.UserID, "In Progress")
		
		fmt.Printf("🚀 Worker %d starting deployment for %s\n", id, task.UserID)
		err := ExecuteRemoteSteps(task.Runner, GetDeploymentSteps(), task.Config)
		
		if err != nil {
			qm.updateStatus(task.UserID, fmt.Sprintf("Failed: %v", err))
		} else {
			qm.updateStatus(task.UserID, "Success")
		}
	}
}

func (qm *QueueManager) Enqueue(task DeploymentTask) {
	qm.updateStatus(task.UserID, "Queued")
	qm.taskChan <- task
}

func (qm *QueueManager) updateStatus(userID, status string) {
	qm.mu.Lock()
	defer qm.mu.Unlock()
	qm.statuses[userID] = status
}

func (qm *QueueManager) GetStatus(userID string) string {
	qm.mu.RLock()
	defer qm.mu.RUnlock()
	if status, ok := qm.statuses[userID]; ok {
		return status
	}
	return "No active deployment"
}
