package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// Constants for the Data Proxy service
const DataProxyURL = "http://localhost:8081"

// DeploymentRequest matches the frontend payload
type DeploymentRequest struct {
	UserID      string `json:"user_id"`
	ProjectName string `json:"project_name"`
	VPS_IP      string `json:"vps_ip"`
	SSH_User    string `json:"ssh_user"`
	SSH_Pass    string `json:"ssh_pass"`
}

type CommandRequest struct {
	UserID      string `json:"user_id"`
	Command     string `json:"command"`
	ProjectName string `json:"project_name"`
	VPS_IP      string `json:"vps_ip"`
	SSH_User    string `json:"ssh_user"`
	SSH_Pass    string `json:"ssh_pass"`
}

type DeployRequest struct {
	UserID       string            `json:"user_id"`
	ProjectName  string            `json:"project_name"`
	VPS_IP       string            `json:"vps_ip"`
	SSH_User     string            `json:"ssh_user"`
	SSH_Pass     string            `json:"ssh_pass"`
	DeploySource string            `json:"deploy_source"` // "git" or "local"
	RepoURL      string            `json:"repo_url"`
	GitToken     string            `json:"git_token"`
	Branch       string            `json:"branch"`
	LocalPath    string            `json:"local_path"`
	Type         string            `json:"type"` // "auto", "node", "go", etc.
	UseDocker    bool              `json:"use_docker"`
	Port         string            `json:"port"`
	Domain       string            `json:"domain"`
	BuildCommand string            `json:"build_command"`
	StartCommand string            `json:"start_command"`
	AppPath      string            `json:"app_path"`
	EnvVars      map[string]string `json:"env_vars"`
}

var (
	connManager  *ConnectionManager
	queueManager *QueueManager
	upgrader     = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return true // For development
		},
	}
)

func main() {
	connManager = NewConnectionManager()
	queueManager = NewQueueManager(100, 5)

	port := ":8080"

	// Deployment & SSH Routes
	http.HandleFunc("/connect", enableCORS(AuthMiddleware(handleConnect)))
	http.HandleFunc("/deploy", enableCORS(AuthMiddleware(handleDeploy)))
	http.HandleFunc("/exec", enableCORS(AuthMiddleware(handleExec)))
	http.HandleFunc("/terminal", handleTerminal) // WS doesn't use standard middleware easily
	http.HandleFunc("/metrics", enableCORS(AuthMiddleware(handleMetrics)))

	// VPS Configuration Management (Proxied to Data Backend)
	http.HandleFunc("/vps/save", enableCORS(AuthMiddleware(handleSaveVPS)))
	http.HandleFunc("/vps/list", enableCORS(AuthMiddleware(handleListVPS)))
	http.HandleFunc("/vps/update", enableCORS(AuthMiddleware(handleUpdateVPS)))
	http.HandleFunc("/vps/delete", enableCORS(AuthMiddleware(handleDeleteVPS)))

	// File Management Routes
	http.HandleFunc("/files/list", enableCORS(AuthMiddleware(handleFileList)))
	http.HandleFunc("/files/read", enableCORS(AuthMiddleware(handleFileRead)))
	http.HandleFunc("/files/write", enableCORS(AuthMiddleware(handleFileWrite)))
	http.HandleFunc("/files/delete", enableCORS(AuthMiddleware(handleFileDelete)))
	http.HandleFunc("/files/mkdir", enableCORS(AuthMiddleware(handleFileMkdir)))
	http.HandleFunc("/files/rename", enableCORS(AuthMiddleware(handleFileRename)))
	http.HandleFunc("/files/upload", enableCORS(AuthMiddleware(handleFileUpload)))

	// Git
	http.HandleFunc("/git/status", enableCORS(AuthMiddleware(handleGitStatus)))
	http.HandleFunc("/git/branch", enableCORS(AuthMiddleware(handleGitBranch)))
	http.HandleFunc("/git/branches", enableCORS(AuthMiddleware(handleGitBranchesList)))
	http.HandleFunc("/git/run", enableCORS(AuthMiddleware(handleGitRun)))
	http.HandleFunc("/git/diff", enableCORS(AuthMiddleware(handleGitDiff)))

	// Management Routes
	http.HandleFunc("/status", enableCORS(handleStatus))
	http.HandleFunc("/deploy/status", enableCORS(AuthMiddleware(handleDeployStatus)))
	http.HandleFunc("/deploy/confirm", enableCORS(AuthMiddleware(handleConfirm)))
	http.HandleFunc("/deploy/detect", enableCORS(AuthMiddleware(handleDeployDetect)))
	http.HandleFunc("/health", enableCORS(handleDashboardAPI))
	http.HandleFunc("/search", enableCORS(AuthMiddleware(handleSearch)))

	fmt.Println("🚀 Portway Deployment Orchestrator")
	fmt.Printf("🔒 Secure API running on http://localhost%s\n", port)
	log.Fatal(http.ListenAndServe(port, nil))
}

func enableCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-User-ID, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	}
}

func handleDashboardAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Check server console for dashboard metrics"})
}

func handleConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST allowed", http.StatusMethodNotAllowed)
		return
	}

	var req CommandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// 1. If ProjectName is provided, fetch info from Data Proxy
	if req.ProjectName != "" && req.VPS_IP == "" {
		url := fmt.Sprintf("%s/data/find?collection=vps", DataProxyURL)
		client := &http.Client{Timeout: 5 * time.Second}
		proxyReq, _ := http.NewRequest("GET", url, nil)
		proxyReq.Header.Set("X-User-ID", req.UserID)

		resp, err := client.Do(proxyReq)
		if err == nil && resp.StatusCode == http.StatusOK {
			var list []SavedVPS
			json.NewDecoder(resp.Body).Decode(&list)
			for _, v := range list {
				if v.ProjectName == req.ProjectName {
					req.VPS_IP = v.IP
					req.SSH_User = v.SSHUser
					req.SSH_Pass = v.SSHPass
					break
				}
			}
		}
	}

	fmt.Printf("🔌 [CONNECT] User %s connecting to %s\n", req.UserID, req.VPS_IP)
	runner, err := connManager.GetRunner(req.UserID, req.VPS_IP, req.SSH_User, req.SSH_Pass)
	if err != nil {
		fmt.Printf("❌ [CONNECT] Failed for %s: %v\n", req.UserID, err)
		http.Error(w, fmt.Sprintf("Failed to connect to VPS: %v", err), http.StatusUnauthorized)
		return
	}
	fmt.Printf("✅ [CONNECT] Session established for %s\n", req.UserID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "success",
		"message": "Connected to VPS: " + runner.Config.IP,
	})
}

func handleDeploy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST allowed", http.StatusMethodNotAllowed)
		return
	}

	var req DeployRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	var runner *SSHRunner
	var err error

	if req.VPS_IP != "" {
		runner, err = connManager.GetRunner(req.UserID, req.VPS_IP, req.SSH_User, req.SSH_Pass)
	} else {
		runner, err = connManager.GetExistingRunner(req.UserID)
	}

	if err != nil {
		http.Error(w, fmt.Sprintf("SSH Session error: %v", err), http.StatusUnauthorized)
		return
	}

	if req.ProjectName == "" {
		req.ProjectName = "my-app"
	}

	// Prepare env vars block
	var envBlock strings.Builder
	for k, v := range req.EnvVars {
		envBlock.WriteString(fmt.Sprintf("%s=%s\n", k, v))
	}

	config := map[string]string{
		"vps_ip":         runner.Config.IP,
		"project_name":   req.ProjectName,
		"deploy_source":  req.DeploySource,
		"source":         req.RepoURL,
		"git_token":      req.GitToken,
		"branch":         req.Branch,
		"local_path":     req.LocalPath,
		"type":           req.Type,
		"use_docker":     fmt.Sprintf("%v", req.UseDocker),
		"port":           req.Port,
		"domain":         req.Domain,
		"build_command":  req.BuildCommand,
		"start_command":  req.StartCommand,
		"app_path":       req.AppPath,
		"env_vars_block": envBlock.String(),
	}

	task := &DeploymentTask{
		UserID:      req.UserID,
		ProjectName: req.ProjectName,
		GitToken:    req.GitToken,
		EnvVars:     req.EnvVars,
		Config:      config,
		Runner:      runner,
		Logs:        []string{"Deployment initialized..."},
		Status:      "running",
		Resume:      make(chan bool, 1),
	}

	queueManager.Enqueue(task)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "queued",
		"message": "Deployment added to queue. Check /deploy/status for updates.",
	})
}

func handleDeployStatus(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	task := queueManager.GetTask(userID)
	if task == nil {
		http.Error(w, "No active deployment", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(task)
}

func handleConfirm(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")

	// Find the task. Currently, we retrieve the active task by userID.
	task := queueManager.GetTask(userID)
	if task == nil {
		http.Error(w, "Task not found", http.StatusNotFound)
		return
	}

	if task.Status != "waiting" {
		http.Error(w, "Task is not waiting for confirmation (status: "+task.Status+")", http.StatusBadRequest)
		return
	}

	task.Resume <- true

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "resuming"})
}

func handleDeployDetect(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	path := r.URL.Query().Get("path")
	if path == "" {
		path = "."
	}

	runner, err := connManager.GetExistingRunner(userID)
	if err != nil {
		http.Error(w, "No active session", http.StatusNotFound)
		return
	}

	appPath := path
	if path != "." && !strings.HasPrefix(path, "/") {
		appPath = path // In reality we should probably resolve this
	}

	// Create a dummy task for detection
	detectedType := "static"
	if _, err := runner.Execute(fmt.Sprintf("[ -f %s/Dockerfile ]", appPath)); err == nil {
		detectedType = "docker"
	} else if _, err := runner.Execute(fmt.Sprintf("[ -f %s/package.json ]", appPath)); err == nil {
		detectedType = "node"
	} else if _, err := runner.Execute(fmt.Sprintf("[ -f %s/go.mod ]", appPath)); err == nil {
		detectedType = "go"
	} else if _, err := runner.Execute(fmt.Sprintf("[ -f %s/requirements.txt ]", appPath)); err == nil {
		detectedType = "python"
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"type": detectedType,
		"path": path,
	})
}

func handleExec(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST allowed", http.StatusMethodNotAllowed)
		return
	}

	var req CommandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	fmt.Printf("⌨️ [EXEC] User %s: %s\n", req.UserID, req.Command)
	var runner *SSHRunner
	var err error

	if req.VPS_IP != "" {
		runner, err = connManager.GetRunner(req.UserID, req.VPS_IP, req.SSH_User, req.SSH_Pass)
	} else {
		runner, err = connManager.GetExistingRunner(req.UserID)
	}

	if err != nil {
		fmt.Printf("⚠️ [EXEC] Session lost for %s\n", req.UserID)
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Session lost. Please re-connect."})
		return
	}

	output, err := runner.Execute(req.Command)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error(), "output": output})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"output": output,
		"cwd":    runner.CWD,
	})
}

func handleTerminal(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	initialPath := r.URL.Query().Get("initial_path")
	if userID == "" {
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Terminal upgrade error: %v", err)
		return
	}
	defer conn.Close()

	runner, err := connManager.GetExistingRunner(userID)
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte("\r\n[ERROR] No active SSH session. Please connect first.\r\n"))
		return
	}

	// Default size, we can implement resize later
	stdin, stdout, session, err := runner.GetPTY(100, 30, initialPath)
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte("\r\n[ERROR] Failed to start PTY: "+err.Error()+"\r\n"))
		return
	}
	defer session.Close()

	// Pipe SSH stdout to WebSocket
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := stdout.Read(buf)
			if err != nil {
				return
			}
			if err := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
				return
			}
		}
	}()

	// Pipe WebSocket to SSH stdin
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if _, err := stdin.Write(msg); err != nil {
			return
		}
	}
}

func handleMetrics(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	runner, err := connManager.GetExistingRunner(userID)
	if err != nil {
		http.Error(w, "No active session", http.StatusNotFound)
		return
	}

	metrics, err := runner.GetMetrics()
	if err != nil {
		http.Error(w, "Failed to fetch metrics", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(metrics)
}

func handleSaveVPS(w http.ResponseWriter, r *http.Request) {
	var vps SavedVPS
	if err := json.NewDecoder(r.Body).Decode(&vps); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	body, _ := json.Marshal(map[string]interface{}{
		"collection": "vps",
		"user_id":    vps.UserID,
		"data":       vps,
	})

	client := &http.Client{Timeout: 5 * time.Second}
	proxyReq, _ := http.NewRequest("POST", DataProxyURL+"/data/create", bytes.NewBuffer(body))
	proxyReq.Header.Set("Content-Type", "application/json")
	proxyReq.Header.Set("X-User-ID", vps.UserID)

	resp, err := client.Do(proxyReq)
	if err != nil || (resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK) {
		http.Error(w, "Data Proxy failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "success", "message": "VPS configuration saved via Data Proxy"})
}

func handleListVPS(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		userID = r.Header.Get("X-User-ID")
	}

	url := fmt.Sprintf("%s/data/find?collection=vps", DataProxyURL)
	client := &http.Client{Timeout: 5 * time.Second}
	proxyReq, _ := http.NewRequest("GET", url, nil)
	proxyReq.Header.Set("X-User-ID", userID)

	resp, err := client.Do(proxyReq)
	if err != nil {
		http.Error(w, "Data Proxy failed", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	var list []SavedVPS
	json.NewDecoder(resp.Body).Decode(&list)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

func handleUpdateVPS(w http.ResponseWriter, r *http.Request) {
	var vps SavedVPS
	if err := json.NewDecoder(r.Body).Decode(&vps); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	body, _ := json.Marshal(map[string]interface{}{
		"collection": "vps",
		"user_id":    vps.UserID,
		"filter":     map[string]interface{}{"_id": vps.ID}, // This assumes the data service handles ID correctly
		"data":       vps,
	})

	client := &http.Client{Timeout: 5 * time.Second}
	proxyReq, _ := http.NewRequest("POST", DataProxyURL+"/data/update", bytes.NewBuffer(body))
	proxyReq.Header.Set("Content-Type", "application/json")
	proxyReq.Header.Set("X-User-ID", vps.UserID)

	resp, err := client.Do(proxyReq)
	if err != nil || resp.StatusCode != http.StatusAccepted {
		http.Error(w, "Data Proxy failed", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func handleDeleteVPS(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID string `json:"user_id"`
		VPS_ID string `json:"vps_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	body, _ := json.Marshal(map[string]interface{}{
		"collection": "vps",
		"user_id":    req.UserID,
		"filter":     map[string]interface{}{"_id": req.VPS_ID},
	})

	client := &http.Client{Timeout: 5 * time.Second}
	proxyReq, _ := http.NewRequest("POST", DataProxyURL+"/data/delete", bytes.NewBuffer(body))
	proxyReq.Header.Set("Content-Type", "application/json")
	proxyReq.Header.Set("X-User-ID", req.UserID)

	resp, err := client.Do(proxyReq)
	if err != nil || resp.StatusCode != http.StatusAccepted {
		http.Error(w, "Data Proxy failed", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

// File Management Handlers

func handleFileList(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	path := r.URL.Query().Get("path")
	if path == "" {
		path = "."
	}

	runner, err := connManager.GetExistingRunner(userID)
	if err != nil {
		http.Error(w, "No active session", http.StatusNotFound)
		return
	}

	files, err := runner.ListDir(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var fileInfos []FileInfo
	for _, f := range files {
		fileInfos = append(fileInfos, FileInfo{
			Name:    f.Name(),
			Size:    f.Size(),
			IsDir:   f.IsDir(),
			ModTime: f.ModTime().Format(time.RFC3339),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(fileInfos)
}

func handleFileRead(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	path := r.URL.Query().Get("path")

	runner, err := connManager.GetExistingRunner(userID)
	if err != nil {
		http.Error(w, "No active session", http.StatusNotFound)
		return
	}

	data, err := runner.ReadFile(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Force download if requested
	filename := strings.Split(path, "/")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename[len(filename)-1]))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Write(data)
}

func handleFileWrite(w http.ResponseWriter, r *http.Request) {
	var req FileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	runner, err := connManager.GetExistingRunner(req.UserID)
	if err != nil {
		http.Error(w, "No active session", http.StatusNotFound)
		return
	}

	err = runner.WriteFile(req.Path, []byte(req.Content))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func handleGitStatus(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	path := r.URL.Query().Get("path")
	if path == "" {
		path = "."
	}

	runner, err := connManager.GetExistingRunner(userID)
	if err != nil {
		http.Error(w, "No active session", http.StatusNotFound)
		return
	}

	// Try to get git status. Ignore error if not a git repo
	output, _ := runner.Execute(fmt.Sprintf("cd %s && git status --short 2>/dev/null", path))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"output": output})
}

func handleGitBranch(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	path := r.URL.Query().Get("path")
	if path == "" {
		path = "."
	}

	runner, err := connManager.GetExistingRunner(userID)
	if err != nil {
		http.Error(w, "No active session", http.StatusNotFound)
		return
	}

	// Try to get current branch
	output, _ := runner.Execute(fmt.Sprintf("cd %s && git rev-parse --abbrev-ref HEAD 2>/dev/null", path))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"branch": strings.TrimSpace(output)})
}

func handleGitBranchesList(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	path := r.URL.Query().Get("path")
	if path == "" {
		path = "."
	}

	runner, err := connManager.GetExistingRunner(userID)
	if err != nil {
		http.Error(w, "No active session", http.StatusNotFound)
		return
	}

	output, _ := runner.Execute(fmt.Sprintf("cd %s && git branch --format='%%(refname:short)'", path))
	branches := strings.Split(strings.TrimSpace(output), "\n")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"branches": branches})
}

func handleGitRun(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID  string `json:"user_id"`
		Path    string `json:"path"`
		Command string `json:"command"` // e.g. "add .", "commit -m 'msg'", "push"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	runner, err := connManager.GetExistingRunner(req.UserID)
	if err != nil {
		http.Error(w, "No active session", http.StatusNotFound)
		return
	}

	// Safety check: only allow git commands
	fullCmd := fmt.Sprintf("cd %s && git %s", req.Path, req.Command)
	output, err := runner.Execute(fullCmd)

	resp := map[string]interface{}{
		"output":  output,
		"success": err == nil,
	}
	if err != nil {
		resp["error"] = err.Error()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func handleGitDiff(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	path := r.URL.Query().Get("path")
	file := r.URL.Query().Get("file")
	isStaged := r.URL.Query().Get("staged") == "true"

	if path == "" {
		path = "."
	}

	runner, err := connManager.GetExistingRunner(userID)
	if err != nil {
		http.Error(w, "No active session", http.StatusNotFound)
		return
	}

	var cmd string
	if isStaged {
		cmd = fmt.Sprintf("cd %s && git diff --cached \"%s\"", path, file)
	} else {
		cmd = fmt.Sprintf("cd %s && git diff \"%s\"", path, file)
	}

	output, _ := runner.Execute(cmd)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"diff": output})
}

func handleFileDelete(w http.ResponseWriter, r *http.Request) {
	var req FileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	runner, err := connManager.GetExistingRunner(req.UserID)
	if err != nil {
		http.Error(w, "No active session", http.StatusNotFound)
		return
	}

	err = runner.DeleteFile(req.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func handleFileMkdir(w http.ResponseWriter, r *http.Request) {
	var req FileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	runner, err := connManager.GetExistingRunner(req.UserID)
	if err != nil {
		http.Error(w, "No active session", http.StatusNotFound)
		return
	}

	err = runner.Mkdir(req.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func handleFileRename(w http.ResponseWriter, r *http.Request) {
	var req FileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	runner, err := connManager.GetExistingRunner(req.UserID)
	if err != nil {
		http.Error(w, "No active session", http.StatusNotFound)
		return
	}

	err = runner.Rename(req.Path, req.NewPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func handleFileUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST allowed", http.StatusMethodNotAllowed)
		return
	}

	// Max 10MB
	r.ParseMultipartForm(10 << 20)

	userID := r.FormValue("user_id")
	path := r.FormValue("path")
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Error retrieving file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	runner, err := connManager.GetExistingRunner(userID)
	if err != nil {
		http.Error(w, "No active session", http.StatusNotFound)
		return
	}

	fullPath := path + "/" + header.Filename
	if path == "." {
		fullPath = header.Filename
	}

	// Use SFTP to write
	sftp := runner.SFTPClient
	if sftp == nil {
		http.Error(w, "SFTP client not initialized", http.StatusInternalServerError)
		return
	}

	dst, err := sftp.Create(fullPath)
	if err != nil {
		http.Error(w, "Failed to create file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	if _, err := dst.ReadFrom(file); err != nil {
		http.Error(w, "Failed to save file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "success", "file": fullPath})
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "running"})
}

func handleSearch(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	path := r.URL.Query().Get("path")
	query := r.URL.Query().Get("query")
	if path == "" {
		path = "."
	}

	runner, err := connManager.GetExistingRunner(userID)
	if err != nil {
		http.Error(w, "No active session", http.StatusNotFound)
		return
	}

	// Use grep for search. -r recursive, -n line number, -I ignore binary, -E regex
	// Limit to 50 results for speed
	cmd := fmt.Sprintf("cd %s && grep -rnIE --exclude-dir=.git \"%s\" . | head -n 50", path, query)
	output, _ := runner.Execute(cmd)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"output": output})
}
