package main

import (
	"fmt"
	"time"
)

func ExecuteRemoteSteps(task *DeploymentTask, steps []Step) error {
	fmt.Printf("\n🚀 Starting Remote Deployment for %s to %s...\n", task.UserID, task.Config["vps_ip"])

	for i, step := range steps {
		task.CurrentStep = i + 1
		task.StepName = step.Name
		
		logMsg := fmt.Sprintf("[%d/%d] %s... ⏳", i+1, len(steps), step.Name)
		task.Logs = append(task.Logs, logMsg)
		fmt.Print(logMsg + " ")

		start := time.Now()
		if step.Name == "User Confirmation" {
			task.Status = "waiting"
			task.Logs = append(task.Logs, "⏸️ Paused: Please confirm the project structure in the dashboard.")
			fmt.Println("⏸️ Paused: Waiting for user confirmation...")
			<-task.Resume // Wait for signal from API
			task.Status = "running"
			task.Logs = append(task.Logs, "▶️ Resuming deployment...")
			fmt.Println("▶️ Resuming deployment...")
		}

		err := step.Run(task, task.Runner, task.Config)
		elapsed := time.Since(start)

		if err != nil {
			failMsg := fmt.Sprintf("❌ Failed in %s | Error: %v", elapsed.Round(time.Millisecond), err)
			task.Logs = append(task.Logs, failMsg)
			fmt.Println(failMsg)
			return err
		}

		successMsg := fmt.Sprintf("Completed in %s ✅", elapsed.Round(time.Millisecond))
		task.Logs = append(task.Logs, successMsg)
		fmt.Println(successMsg)

		// Pause for confirmation after each step to ensure user is following
		if i < len(steps)-1 {
			task.Status = "waiting"
			task.Logs = append(task.Logs, fmt.Sprintf("⏸️ Step %d complete. Waiting for confirmation to proceed to: %s", i+1, steps[i+1].Name))
			<-task.Resume
			task.Status = "running"
		}
	}

	finishMsg := "✨ Remote Deployment completed successfully!"
	task.Logs = append(task.Logs, finishMsg)
	fmt.Println("\n" + finishMsg)
	return nil
}

