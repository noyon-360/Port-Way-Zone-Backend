package main

import (
	"fmt"
	"sync"
)

type DeploymentTask struct {
	UserID      string
	ProjectName string
	GitToken    string
	EnvVars     map[string]string
	Config      map[string]string
	Runner      *SSHRunner
	CurrentStep int
	TotalSteps  int
	StepName    string
	Logs        []string
	Status      string
	Resume      chan bool // Channel to signal resumption from pause
}

type QueueManager struct {
	taskChan   chan *DeploymentTask
	tasks      map[string]*DeploymentTask // userID -> task
	mu         sync.RWMutex
	maxWorkers int
}

func NewQueueManager(bufferSize, workers int) *QueueManager {
	qm := &QueueManager{
		taskChan:   make(chan *DeploymentTask, bufferSize),
		tasks:      make(map[string]*DeploymentTask),
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
		task.Status = "In Progress"
		
		fmt.Printf("🚀 Worker %d starting deployment for %s\n", id, task.UserID)
		steps := GetDeploymentSteps()
		task.TotalSteps = len(steps)
		
		err := ExecuteRemoteSteps(task, steps)
		
		if err != nil {
			task.Status = fmt.Sprintf("Failed: %v", err)
		} else {
			task.Status = "Success"
		}
	}
}

func (qm *QueueManager) Enqueue(task *DeploymentTask) {
	qm.mu.Lock()
	task.Status = "Queued"
	qm.tasks[task.UserID] = task
	qm.mu.Unlock()
	qm.taskChan <- task
}

func (qm *QueueManager) GetTask(userID string) *DeploymentTask {
	qm.mu.RLock()
	defer qm.mu.RUnlock()
	return qm.tasks[userID]
}

