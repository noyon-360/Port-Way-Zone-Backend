package main

import (
	"fmt"
)

type Step struct {
	Name string
	Run  func(runner *SSHRunner, config map[string]string) error
}

func GetDeploymentSteps() []Step {
	return []Step{
		{
			Name: "Preparation & Dependencies",
			Run: func(runner *SSHRunner, config map[string]string) error {
				projType := config["type"]
				baseDeps := "sudo apt update && sudo apt install -y nginx git certbot python3-certbot-nginx"
				
				switch projType {
				case "node":
					baseDeps += " curl && curl -fsSL https://deb.nodesource.com/setup_lts.x | sudo -E bash - && sudo apt install -y nodejs"
				case "go":
					baseDeps += " golang-go"
				case "python":
					baseDeps += " python3-pip python3-venv"
				}
				
				_, err := runner.Execute(baseDeps)
				return err
			},
		},
		{
			Name: "Source Control Sync",
			Run: func(runner *SSHRunner, config map[string]string) error {
				repo := config["source"]
				branch := config["branch"]
				if branch == "" { branch = "main" }
				projectName := config["project_name"]
				if projectName == "" { projectName = "my-app" }
				
				appPath := fmt.Sprintf("/var/www/%s", projectName)
				cmd := fmt.Sprintf("sudo mkdir -p /var/www && sudo rm -rf %s && sudo git clone -b %s %s %s", appPath, branch, repo, appPath)
				_, err := runner.Execute(cmd)
				return err
			},
		},
		{
			Name: "Build Phase",
			Run: func(runner *SSHRunner, config map[string]string) error {
				projType := config["type"]
				projectName := config["project_name"]
				appPath := fmt.Sprintf("/var/www/%s", projectName)
				
				buildCommand := config["build_command"]
				if buildCommand == "" {
					switch projType {
					case "node":
						buildCommand = "npm install && npm run build --if-present"
					case "go":
						buildCommand = "go build -o app"
					case "python":
						buildCommand = "pip3 install -r requirements.txt"
					}
				}

				if buildCommand != "" {
					_, err := runner.Execute(fmt.Sprintf("cd %s && sudo %s", appPath, buildCommand))
					return err
				}
				return nil
			},
		},
		{
			Name: "Process Management (systemd)",
			Run: func(runner *SSHRunner, config map[string]string) error {
				projType := config["type"]
				if projType == "static" {
					return nil // No service needed for static sites
				}

				projectName := config["project_name"]
				appPath := fmt.Sprintf("/var/www/%s", projectName)
				port := config["port"]
				
				startCommand := config["start_command"]
				if startCommand == "" {
					switch projType {
					case "node":
						startCommand = "/usr/bin/npm start"
					case "go":
						startCommand = "./app"
					case "python":
						startCommand = "python3 main.py"
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
			Name: "Traffic Routing (Nginx)",
			Run: func(runner *SSHRunner, config map[string]string) error {
				projType := config["type"]
				projectName := config["project_name"]
				domain := config["domain"]
				port := config["port"]
				appPath := fmt.Sprintf("/var/www/%s", projectName)
				
				if domain == "" { domain = "_" }

				var locationBlock string
				if projType == "static" {
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
    }`, port)
				}

				nginxConf := fmt.Sprintf(`
server {
    listen 80;
    server_name %s;
    %s
}`, domain, locationBlock)

				cmd := fmt.Sprintf("echo '%s' | sudo tee /etc/nginx/sites-available/%s && sudo ln -sf /etc/nginx/sites-available/%s /etc/nginx/sites-enabled/ && sudo nginx -t && sudo systemctl restart nginx", nginxConf, projectName, projectName)
				_, err := runner.Execute(cmd)
				return err
			},
		},
		{
			Name: "Security (SSL)",
			Run: func(runner *SSHRunner, config map[string]string) error {
				domain := config["domain"]
				if domain == "" || domain == "_" {
					return nil
				}
				_, err := runner.Execute(fmt.Sprintf("sudo certbot --nginx -d %s --non-interactive --agree-tos -m dev@portway.com", domain))
				return err
			},
		},
	}
}
