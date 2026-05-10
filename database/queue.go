package main

import (
	"context"
	"fmt"
	"time"
)

// TaskType defines the operation to perform
type TaskType string

const (
	TaskCreate TaskType = "CREATE"
	TaskUpdate TaskType = "UPDATE"
	TaskDelete TaskType = "DELETE"
)

// DBTask represents a database operation queued for processing
type DBTask struct {
	Type       TaskType
	Collection string
	Filter     interface{}
	Data       interface{}
	UserID     string
	Timestamp  time.Time
}

type QueueManager struct {
	TaskQueue chan DBTask
	Workers   int
	Store     DataStore
}

func NewQueueManager(workers int, store DataStore) *QueueManager {
	return &QueueManager{
		TaskQueue: make(chan DBTask, 1000), // Buffer for 1000 tasks
		Workers:   workers,
		Store:     store,
	}
}

func (q *QueueManager) Start() {
	for i := 0; i < q.Workers; i++ {
		go q.worker(i)
	}
}

func (q *QueueManager) worker(id int) {
	fmt.Printf("👷 DB-Worker %d started\n", id)
	for task := range q.TaskQueue {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		
		var err error
		switch task.Type {
		case TaskCreate:
			_, err = q.Store.Create(ctx, task.Collection, task.Data)
		case TaskUpdate:
			err = q.Store.Update(ctx, task.Collection, task.Filter, task.Data)
		case TaskDelete:
			err = q.Store.Delete(ctx, task.Collection, task.Filter)
		}

		if err != nil {
			fmt.Printf("❌ [Worker %d] Task %s on %s failed: %v\n", id, task.Type, task.Collection, err)
		} else {
			fmt.Printf("✅ [Worker %d] Task %s on %s completed for User %s\n", id, task.Type, task.Collection, task.UserID)
		}
		
		cancel()
	}
}

func (q *QueueManager) Enqueue(task DBTask) {
	task.Timestamp = time.Now()
	q.TaskQueue <- task
}
