package main

import (
	"fmt"
	"time"
)

func ExecuteRemoteSteps(runner *SSHRunner, steps []Step, config map[string]string) error {
	fmt.Printf("\n🚀 Starting Remote Deployment to %s...\n", config["vps_ip"])

	for i, step := range steps {
		fmt.Printf("[%d/%d] %s... ⏳ ", i+1, len(steps), step.Name)

		start := time.Now()
		err := step.Run(runner, config)
		elapsed := time.Since(start)

		if err != nil {
			fmt.Printf("❌ Failed in %s\nError: %v\n", elapsed.Round(time.Millisecond), err)
			return err
		}

		fmt.Printf("Completed in %s ✅\n", elapsed.Round(time.Millisecond))
	}

	fmt.Println("\n✨ Remote Deployment completed successfully!")
	return nil
}
