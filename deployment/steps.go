package main

import (
	"fmt"
	"strings"
)

type Step struct {
	Name string
	Run  func(task *DeploymentTask, runner *SSHRunner, config map[string]string) error
}

func getAppPath(config map[string]string) string {
	if path, ok := config["app_path"]; ok && path != "" {
		return path
	}
	projectName := config["project_name"]
	if projectName == "" {
		projectName = "my-app"
	}
	return fmt.Sprintf("/var/www/%s", projectName)
}

func GetDeploymentSteps() []Step {
	return []Step{
		{
			Name: "Resource Preparation",
			Run: func(task *DeploymentTask, runner *SSHRunner, config map[string]string) error {
				fmt.Println("Installing system dependencies (git, nginx)...")
				_, err := runner.Execute("sudo apt-get update && sudo apt-get install -y git nginx")
				return err
			},
		},
		{
			Name: "Environment Setup",
			Run: func(task *DeploymentTask, runner *SSHRunner, config map[string]string) error {
				projType := config["type"]
				// Basic dependencies
				baseDeps := "sudo apt update && sudo apt install -y nginx git certbot python3-certbot-nginx curl"
				
				if config["use_docker"] == "true" {
					baseDeps += " && (docker -v || (curl -fsSL https://get.docker.com -o get-docker.sh && sudo sh get-docker.sh))"
				}

				switch projType {
				case "node":
					baseDeps += " && (node -v || (curl -fsSL https://deb.nodesource.com/setup_lts.x | sudo -E bash - && sudo apt install -y nodejs))"
				case "go":
					baseDeps += " && (go version || sudo apt install -y golang-go)"
				case "python":
					baseDeps += " && sudo apt install -y python3-pip python3-venv"
				case "php":
					baseDeps += " && sudo apt install -y php-fpm php-curl php-gd php-mbstring php-xml php-zip"
				case "rust":
					baseDeps += " && (cargo --version || (curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y))"
				}
				
				_, err := runner.Execute(baseDeps)
				return err
			},
		},
		{
			Name: "Source Control & Sync",
			Run: func(task *DeploymentTask, runner *SSHRunner, config map[string]string) error {
				appPath := getAppPath(config)
				
				// Handle Local vs Git
				if config["deploy_source"] == "local" {
					localPath := config["local_path"]
					if localPath == "" { return fmt.Errorf("local path not provided") }
					if localPath != appPath {
						_, err := runner.Execute(fmt.Sprintf("sudo mkdir -p %s && sudo cp -r %s/. %s/", strings.TrimSuffix(appPath, "/"+config["project_name"]), localPath, appPath))
						return err
					}
					return nil
				}

				// Git Sync (Idempotent + Private Repo Support)
				repo := config["source"]
				if repo == "" { return fmt.Errorf("repository URL is empty") }

				token := config["git_token"]
				if token != "" {
					// Inject token into URL: https://TOKEN@github.com/...
					if strings.HasPrefix(repo, "https://") {
						repo = "https://" + token + "@" + strings.TrimPrefix(repo, "https://")
					}
				}

				branch := config["branch"]
				if branch == "" { branch = "main" }
				
				checkCmd := fmt.Sprintf("[ -d %s/.git ]", appPath)
				_, err := runner.Execute(checkCmd)
				if err == nil {
					fmt.Printf("Updating existing repo at %s\n", appPath)
					task.Logs = append(task.Logs, "Found existing repository. Syncing...")
					_, err = runner.Execute(fmt.Sprintf("cd %s && sudo git fetch origin %s && sudo git reset --hard origin/%s && sudo git clean -fd", appPath, branch, branch))
				} else {
					fmt.Printf("Cloning repo to %s\n", appPath)
					task.Logs = append(task.Logs, fmt.Sprintf("Cloning %s (%s) into %s...", repo, branch, appPath))
					// Ensure parent dir exists and is clean
					runner.Execute(fmt.Sprintf("sudo mkdir -p %s && sudo rm -rf %s", strings.TrimSuffix(appPath, "/"+config["project_name"]), appPath))
					_, err = runner.Execute(fmt.Sprintf("sudo git clone -b %s %s %s", branch, repo, appPath))
				}

				if err != nil {
					return fmt.Errorf("git operation failed: %v", err)
				}

				// Validation: Ensure directory is not empty
				checkEmpty, _ := runner.Execute(fmt.Sprintf("ls -A %s | wc -l", appPath))
				if strings.TrimSpace(checkEmpty) == "0" {
					return fmt.Errorf("cloned repository is empty at %s", appPath)
				}
				
				task.Logs = append(task.Logs, "Source code synchronized successfully.")
				return nil
			},
		},
		{
			Name: "User Confirmation",
			Run: func(task *DeploymentTask, runner *SSHRunner, config map[string]string) error {
				appPath := getAppPath(config)
				// Fetch file list to show user
				files, _ := runner.Execute(fmt.Sprintf("ls -F %s", appPath))
				config["_file_list"] = files // Store for frontend to read if needed
				return nil
			},
		},
		{
			Name: "Environment Configuration",
			Run: func(task *DeploymentTask, runner *SSHRunner, config map[string]string) error {
				appPath := getAppPath(config)
				
				// Extract env vars from config (passed as JSON or serialized string)
				// For now, let's assume they are passed in a specific format or directly as a block
				envData := config["env_vars_block"]
				if envData == "" {
					return nil // No env vars to write
				}

				// Write to .env file
				cmd := fmt.Sprintf("echo '%s' | sudo tee %s/.env", envData, appPath)
				_, err := runner.Execute(cmd)
				return err
			},
		},
		{
			Name: "Project Detection",
			Run: func(task *DeploymentTask, runner *SSHRunner, config map[string]string) error {
				appPath := getAppPath(config)
				
				// Auto-detect type if not set or set to 'auto'
				if config["type"] == "" || config["type"] == "auto" {
					// Check for Dockerfile
					if _, err := runner.Execute(fmt.Sprintf("[ -f %s/Dockerfile ]", appPath)); err == nil {
						config["type"] = "docker"
						config["use_docker"] = "true"
					} else if _, err := runner.Execute(fmt.Sprintf("[ -f %s/package.json ]", appPath)); err == nil {
						config["type"] = "node"
					} else if _, err := runner.Execute(fmt.Sprintf("[ -f %s/go.mod ]", appPath)); err == nil {
						config["type"] = "go"
					} else if _, err := runner.Execute(fmt.Sprintf("[ -f %s/requirements.txt ]", appPath)); err == nil {
						config["type"] = "python"
					} else if _, err := runner.Execute(fmt.Sprintf("[ -f %s/composer.json ]", appPath)); err == nil {
						config["type"] = "php"
					} else if _, err := runner.Execute(fmt.Sprintf("[ -f %s/Cargo.toml ]", appPath)); err == nil {
						config["type"] = "rust"
					} else {
						config["type"] = "static"
					}
				}
				return nil
			},
		},
		{
			Name: "Build & Dependencies",
			Run: func(task *DeploymentTask, runner *SSHRunner, config map[string]string) error {
				projType := config["type"]
				appPath := getAppPath(config)
				projectName := config["project_name"]
				
				if config["use_docker"] == "true" {
					// Build docker image
					_, err := runner.Execute(fmt.Sprintf("cd %s && sudo docker build -t %s .", appPath, projectName))
					return err
				}

				buildCommand := config["build_command"]
				if buildCommand == "" {
					switch projType {
					case "node":
						buildCommand = "npm install && npm run build --if-present"
					case "go":
						buildCommand = "go build -o app"
					case "python":
						buildCommand = "python3 -m venv venv && ./venv/bin/pip install -r requirements.txt"
					case "rust":
						buildCommand = "cargo build --release"
					case "php":
						buildCommand = "composer install --no-dev --optimize-autoloader || true"
					}
				}

				if buildCommand != "" {
					_, err := runner.Execute(fmt.Sprintf("cd %s && %s", appPath, buildCommand))
					return err
				}
				return nil
			},
		},
		{
			Name: "Port Verification",
			Run: func(task *DeploymentTask, runner *SSHRunner, config map[string]string) error {
				port := config["port"]
				if port == "" || config["type"] == "static" {
					return nil
				}
				
				// Check if port is in use
				output, _ := runner.Execute(fmt.Sprintf("sudo lsof -i :%s | grep LISTEN", port))
				if output != "" {
					// Port is in use. We should probably try to kill the existing process IF it's our own app
					// For now, let's just log it. In a real system we'd check if the PID belongs to the previous deployment.
					fmt.Printf("Warning: Port %s is already in use. Attempting to stop previous version if applicable.\n", port)
					runner.Execute(fmt.Sprintf("sudo systemctl stop %s.service || true", config["project_name"]))
					runner.Execute(fmt.Sprintf("sudo docker stop %s || true", config["project_name"]))
				}
				return nil
			},
		},
		{
			Name: "Process Launch",
			Run: func(task *DeploymentTask, runner *SSHRunner, config map[string]string) error {
				projectName := config["project_name"]
				appPath := getAppPath(config)
				port := config["port"]
				
				if config["use_docker"] == "true" {
					// Run via Docker
					_, err := runner.Execute(fmt.Sprintf("sudo docker rm -f %s || true && sudo docker run -d --name %s --restart always -p %s:%s %s", projectName, projectName, port, port, projectName))
					return err
				}

				if config["type"] == "static" {
					return nil
				}

				startCommand := config["start_command"]
				if startCommand == "" {
					switch config["type"] {
					case "node":
						startCommand = "npm start"
					case "go":
						startCommand = "./app"
					case "python":
						startCommand = "./venv/bin/python main.py"
					case "rust":
						startCommand = "./target/release/app"
					case "php":
						startCommand = "php-fpm" // Usually handled differently, but for simplicity
					}
				}

				serviceConf := fmt.Sprintf(`[Unit]
Description=Portway App: %s
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=%s
ExecStart=%s
Restart=always
Environment=PORT=%s

[Install]
WantedBy=multi-user.target`, projectName, appPath, startCommand, port)

				cmd := fmt.Sprintf("echo '%s' | sudo tee /etc/systemd/system/%s.service && sudo systemctl daemon-reload && sudo systemctl enable %s && sudo systemctl restart %s", serviceConf, projectName, projectName, projectName)
				_, err := runner.Execute(cmd)
				return err
			},
		},
		{
			Name: "Nginx Configuration",
			Run: func(task *DeploymentTask, runner *SSHRunner, config map[string]string) error {
				projectName := config["project_name"]
				domain := config["domain"]
				port := config["port"]
				appPath := getAppPath(config)
				
				if domain == "" { domain = "_" }

				var locationBlock string
				if config["type"] == "static" {
					locationBlock = fmt.Sprintf(`
    root %s;
    index index.html;
    location / {
        try_files $uri $uri/ /index.html;
    }`, appPath)
				} else {
					locationBlock = fmt.Sprintf(`
    location / {
        proxy_pass http://localhost:%s;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection 'upgrade';
        proxy_set_header Host $host;
        proxy_cache_bypass $http_upgrade;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }`, port)
				}

				conf := fmt.Sprintf(`server {
    listen 80;
    server_name %s;
    %s
}`, domain, locationBlock)

				// Disable default config to avoid port 80 conflicts
				runner.Execute("sudo rm -f /etc/nginx/sites-enabled/default")
				
				cmd := fmt.Sprintf("echo '%s' | sudo tee /etc/nginx/sites-available/%s && sudo ln -sf /etc/nginx/sites-available/%s /etc/nginx/sites-enabled/ && sudo nginx -t && sudo systemctl restart nginx", conf, projectName, projectName)
				_, err := runner.Execute(cmd)
				return err
			},
		},
		{
			Name: "Final Security (SSL)",
			Run: func(task *DeploymentTask, runner *SSHRunner, config map[string]string) error {
				domain := config["domain"]
				if domain == "" || domain == "_" {
					return nil
				}
				// Check if domain points to this IP
				_, err := runner.Execute(fmt.Sprintf("sudo certbot --nginx -d %s --non-interactive --agree-tos -m dev@portway.com", domain))
				return err
			},
		},
	}
}

