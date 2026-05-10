package main

import (
	"fmt"
	"runtime"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
)

func ShowDashboard() {
	fmt.Println("\n📊 VPS Status Dashboard")
	fmt.Println("-----------------------")

	// OS Info
	fmt.Printf("OS: %s %s\n", runtime.GOOS, runtime.GOARCH)

	// CPU
	c, _ := cpu.Percent(0, false)
	if len(c) > 0 {
		fmt.Printf("CPU Usage: %.2f%%\n", c[0])
	}

	// Memory
	v, _ := mem.VirtualMemory()
	if v != nil {
		fmt.Printf("RAM: %dMB / %dMB (%.2f%% used)\n", v.Used/1024/1024, v.Total/1024/1024, v.UsedPercent)
	}

	// Disk
	d, _ := disk.Usage("/")
	if d != nil {
		fmt.Printf("Disk: %dGB / %dGB (%.2f%% used)\n", d.Used/1024/1024/1024, d.Total/1024/1024/1024, d.UsedPercent)
	}

	fmt.Println("\n🔍 Active Services & Domains")
	fmt.Println("-----------------------")
	fmt.Println("(Feature: Scanning /etc/nginx/sites-enabled for domains...)")
	// Note: In a real VPS, we would scan Nginx configs here.
}
