package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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
	http.HandleFunc("/vps/connect", AuthMiddleware(handleConnect))
	http.HandleFunc("/deploy", AuthMiddleware(handleDeploy))
	http.HandleFunc("/exec", AuthMiddleware(handleExec))
	http.HandleFunc("/terminal", handleTerminal)
	http.HandleFunc("/metrics", AuthMiddleware(handleMetrics))

	// VPS Configuration Management (Proxied to Data Backend)
	http.HandleFunc("/vps/save", AuthMiddleware(handleSaveVPS))
	http.HandleFunc("/vps/list", AuthMiddleware(handleListVPS))
	http.HandleFunc("/vps/update", AuthMiddleware(handleUpdateVPS))
	http.HandleFunc("/vps/delete", AuthMiddleware(handleDeleteVPS))

	// File Management Routes
	http.HandleFunc("/files/list", AuthMiddleware(handleFileList))
	http.HandleFunc("/files/read", AuthMiddleware(handleFileRead))
	http.HandleFunc("/files/write", AuthMiddleware(handleFileWrite))
	http.HandleFunc("/files/delete", AuthMiddleware(handleFileDelete))
	http.HandleFunc("/files/mkdir", AuthMiddleware(handleFileMkdir))
	http.HandleFunc("/files/rename", AuthMiddleware(handleFileRename))
	http.HandleFunc("/files/upload", AuthMiddleware(handleFileUpload))

	// Git
	http.HandleFunc("/git/status", AuthMiddleware(handleGitStatus))
	http.HandleFunc("/git/branch", AuthMiddleware(handleGitBranch))
	http.HandleFunc("/git/branches", AuthMiddleware(handleGitBranchesList))
	http.HandleFunc("/git/run", AuthMiddleware(handleGitRun))
	http.HandleFunc("/git/diff", AuthMiddleware(handleGitDiff))

	// Management Routes
	http.HandleFunc("/status", handleStatus)
	http.HandleFunc("/deploy/status", AuthMiddleware(handleDeployStatus))
	http.HandleFunc("/deploy/confirm", AuthMiddleware(handleConfirm))
	http.HandleFunc("/deploy/detect", AuthMiddleware(handleDeployDetect))
	http.HandleFunc("/health", handleDashboardAPI)
	http.HandleFunc("/search", AuthMiddleware(handleSearch))

	fmt.Println("🚀 Portway Deployment Orchestrator")
	fmt.Printf("🔒 Secure API running on http://localhost%s\n", port)
	log.Fatal(http.ListenAndServe(port, nil))
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

	// If ProjectName is provided, fetch info from Data Proxy
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

	runner, err := connManager.GetExistingRunner(req.UserID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	output, err := runner.Execute(req.Command)
	if err != nil {
		http.Error(w, fmt.Sprintf("Command failed: %v\n%s", err, output), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"output": output,
		"cwd":    runner.CWD,
	})
}

func handleTerminal(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WS Upgrade Error: %v", err)
		return
	}
	defer ws.Close()

	// 1. Authenticate via URL params
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		ws.WriteMessage(websocket.TextMessage, []byte("Error: user_id required"))
		return
	}

	runner, err := connManager.GetExistingRunner(userID)
	if err != nil {
		ws.WriteMessage(websocket.TextMessage, []byte("Error: No active SSH session. Please connect first."))
		return
	}

	// 2. Start PTY
	stdin, stdout, session, err := runner.GetPTY(80, 24, ".")
	if err != nil {
		ws.WriteMessage(websocket.TextMessage, []byte("Error: Failed to start PTY: "+err.Error()))
		return
	}
	defer session.Close()

	// 3. Pipe logic
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := stdout.Read(buf)
			if err != nil {
				return
			}
			ws.WriteMessage(websocket.TextMessage, buf[:n])
		}
	}()

	for {
		_, msg, err := ws.ReadMessage()
		if err != nil {
			break
		}
		stdin.Write(msg)
	}
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
		if err != nil {
			http.Error(w, "Failed to connect to VPS: "+err.Error(), http.StatusUnauthorized)
			return
		}
	} else {
		runner, err = connManager.GetExistingRunner(req.UserID)
		if err != nil {
			http.Error(w, "No active session. Please connect first.", http.StatusNotFound)
			return
		}
	}

	// Convert DeployRequest to map for DeploymentTask
	config := make(map[string]string)
	config["vps_ip"] = runner.Config.IP
	config["project_name"] = req.ProjectName
	config["deploy_source"] = req.DeploySource
	config["source"] = req.RepoURL
	config["git_token"] = req.GitToken
	config["branch"] = req.Branch
	config["local_path"] = req.LocalPath
	config["type"] = req.Type
	config["use_docker"] = fmt.Sprintf("%v", req.UseDocker)
	config["port"] = req.Port
	config["domain"] = req.Domain
	config["build_command"] = req.BuildCommand
	config["start_command"] = req.StartCommand
	config["app_path"] = req.AppPath

	task := &DeploymentTask{
		UserID:      req.UserID,
		ProjectName: req.ProjectName,
		GitToken:    req.GitToken,
		EnvVars:     req.EnvVars,
		Config:      config,
		Runner:      runner,
		Status:      "Queued",
		Resume:      make(chan bool, 1),
	}

	queueManager.Enqueue(task)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "queued",
		"message": "Deployment started in background",
	})
}

func handleDeployStatus(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	task := queueManager.GetTask(userID)
	if task == nil {
		http.Error(w, "No active deployment found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(task)
}

func handleConfirm(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	task := queueManager.GetTask(userID)
	if task == nil {
		http.Error(w, "No active deployment found", http.StatusNotFound)
		return
	}

	select {
	case task.Resume <- true:
		json.NewEncoder(w).Encode(map[string]string{"status": "resumed"})
	default:
		json.NewEncoder(w).Encode(map[string]string{"status": "already_resumed"})
	}
}

func handleMetrics(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	runner, err := connManager.GetExistingRunner(userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	metrics, err := runner.GetMetrics()
	if err != nil {
		http.Error(w, "Failed to fetch metrics: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(metrics)
}

// File Management

func handleFileList(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	path := r.URL.Query().Get("path")
	runner, err := connManager.GetExistingRunner(userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	files, err := runner.ListDir(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var info []FileInfo
	for _, f := range files {
		info = append(info, FileInfo{
			Name:    f.Name(),
			Size:    f.Size(),
			IsDir:   f.IsDir(),
			ModTime: f.ModTime().Format(time.RFC3339),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}

func handleFileRead(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	path := r.URL.Query().Get("path")
	runner, err := connManager.GetExistingRunner(userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	data, err := runner.ReadFile(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	w.Write(data)
}

func handleFileWrite(w http.ResponseWriter, r *http.Request) {
	var req FileRequest
	json.NewDecoder(r.Body).Decode(&req)
	runner, err := connManager.GetExistingRunner(req.UserID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	err = runner.WriteFile(req.Path, []byte(req.Content))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func handleFileDelete(w http.ResponseWriter, r *http.Request) {
	var req FileRequest
	json.NewDecoder(r.Body).Decode(&req)
	runner, err := connManager.GetExistingRunner(req.UserID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	err = runner.DeleteFile(req.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func handleFileMkdir(w http.ResponseWriter, r *http.Request) {
	var req FileRequest
	json.NewDecoder(r.Body).Decode(&req)
	runner, err := connManager.GetExistingRunner(req.UserID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	err = runner.Mkdir(req.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func handleFileRename(w http.ResponseWriter, r *http.Request) {
	var req FileRequest
	json.NewDecoder(r.Body).Decode(&req)
	runner, err := connManager.GetExistingRunner(req.UserID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	err = runner.Rename(req.Path, req.NewPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func handleFileUpload(w http.ResponseWriter, r *http.Request) {
	// Simple implementation
	r.ParseMultipartForm(10 << 20) // 10MB
	userID := r.FormValue("user_id")
	path := r.FormValue("path")

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()

	runner, err := connManager.GetExistingRunner(userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	content, _ := io.ReadAll(file)
	err = runner.WriteFile(path+"/"+header.Filename, content)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// Git Helpers

func handleGitStatus(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	runner, err := connManager.GetExistingRunner(userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	out, _ := runner.Execute("git status")
	json.NewEncoder(w).Encode(map[string]string{"output": out})
}

func handleGitBranch(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	runner, err := connManager.GetExistingRunner(userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	out, _ := runner.Execute("git branch --show-current")
	json.NewEncoder(w).Encode(map[string]string{"branch": strings.TrimSpace(out)})
}

func handleGitBranchesList(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	runner, err := connManager.GetExistingRunner(userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	out, _ := runner.Execute("git branch -a")
	json.NewEncoder(w).Encode(map[string]string{"branches": out})
}

func handleGitRun(w http.ResponseWriter, r *http.Request) {
	var req CommandRequest
	json.NewDecoder(r.Body).Decode(&req)
	runner, err := connManager.GetExistingRunner(req.UserID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	out, err := runner.Execute("git " + req.Command)
	if err != nil {
		http.Error(w, out, http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"output": out})
}

func handleGitDiff(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	runner, err := connManager.GetExistingRunner(userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	out, _ := runner.Execute("git diff")
	json.NewEncoder(w).Encode(map[string]string{"output": out})
}

// VPS Persistence (Proxy to Database Service)

func handleSaveVPS(w http.ResponseWriter, r *http.Request) {
	// 1. Read body to get user_id if needed, but we also proxy it
	bodyBytes, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes)) // Reset body for proxying
	
	// 2. Proxy to database service
	client := &http.Client{}
	url := fmt.Sprintf("%s/data/create?collection=vps", DataProxyURL)
	proxyReq, _ := http.NewRequest(r.Method, url, bytes.NewBuffer(bodyBytes))
	proxyReq.Header = r.Header
	
	resp, err := client.Do(proxyReq)
	if err != nil {
		fmt.Printf("❌ [DEPLOY] Database save failed: %v\n", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func handleListVPS(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		userID = r.Header.Get("X-User-ID")
	}
	
	fmt.Printf("🔍 [DEPLOY] Listing VPS for User: %s\n", userID)
	
	client := &http.Client{}
	url := fmt.Sprintf("%s/data/find?collection=vps", DataProxyURL)
	proxyReq, _ := http.NewRequest("GET", url, nil)
	proxyReq.Header.Set("X-User-ID", userID)
	
	resp, err := client.Do(proxyReq)
	if err != nil {
		fmt.Printf("❌ [DEPLOY] Database list failed: %v\n", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func handleUpdateVPS(w http.ResponseWriter, r *http.Request) {
	client := &http.Client{}
	proxyReq, _ := http.NewRequest(r.Method, DataProxyURL+"/data/update?collection=vps", r.Body)
	proxyReq.Header = r.Header
	resp, err := client.Do(proxyReq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func handleDeleteVPS(w http.ResponseWriter, r *http.Request) {
	client := &http.Client{}
	proxyReq, _ := http.NewRequest(r.Method, DataProxyURL+"/data/delete?collection=vps", r.Body)
	proxyReq.Header = r.Header
	resp, err := client.Do(proxyReq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "running"})
}

func handleDeployDetect(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	runner, err := connManager.GetExistingRunner(userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	appPath := r.URL.Query().Get("path")
	if appPath == "" {
		appPath = "."
	}

	config := make(map[string]string)
	config["app_path"] = appPath

	task := &DeploymentTask{Runner: runner, Config: config}
	steps := GetDeploymentSteps()
	// Run only the detection step
	for _, step := range steps {
		if step.Name == "Project Detection" {
			step.Run(task, runner, config)
			break
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"type": config["type"]})
}

func handleSearch(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	query := r.URL.Query().Get("q")
	runner, err := connManager.GetExistingRunner(userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	out, _ := runner.Execute(fmt.Sprintf("grep -r %q .", query))
	json.NewEncoder(w).Encode(map[string]string{"output": out})
}
