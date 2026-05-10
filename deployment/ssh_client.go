package main

import (
	"bytes"
	"fmt"
	"golang.org/x/crypto/ssh"
	"io"
	"os"
	"strings"
	"time"

	"github.com/pkg/sftp"
)

type SSHConfig struct {
	IP       string
	User     string
	Password string
}

type SSHRunner struct {
	Client     *ssh.Client
	SFTPClient *sftp.Client
	Config     SSHConfig
	CWD        string // Current Working Directory
}

type SystemMetrics struct {
	CPU  string `json:"cpu"`
	RAM  string `json:"ram"`
	Disk string `json:"disk"`
}

func NewSSHRunner(ip, user, password string) (*SSHRunner, error) {
	sshConfig := SSHConfig{IP: ip, User: user, Password: password}
	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.Password(password),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         15 * time.Second,
	}

	client, err := ssh.Dial("tcp", ip+":22", config)
	if err != nil {
		return nil, err
	}

	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		client.Close()
		return nil, err
	}

	return &SSHRunner{Client: client, SFTPClient: sftpClient, Config: sshConfig, CWD: "~"}, nil
}

// Execute runs a command and returns the output, while maintaining CWD
func (r *SSHRunner) Execute(command string) (string, error) {
	session, err := r.Client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	// Robust wrapping to keep track of CWD
	wrappedCmd := fmt.Sprintf("cd %s; %s; echo; echo '___PWD_START___'; pwd", r.CWD, command)

	err = session.Run(wrappedCmd)
	
	rawOutput := stdout.String()
	errorOutput := stderr.String()

	// Parse for the new CWD
	outputParts := strings.Split(rawOutput, "___PWD_START___")
	var finalOutput string
	
	if len(outputParts) > 1 {
		finalOutput = strings.TrimSpace(outputParts[0])
		r.CWD = strings.TrimSpace(outputParts[1])
	} else {
		finalOutput = strings.TrimSpace(rawOutput)
	}

	if err != nil {
		if finalOutput != "" {
			finalOutput += "\n"
		}
		finalOutput += strings.TrimSpace(errorOutput)
	}

	return finalOutput, nil
}

// GetPTY starts an interactive shell with a PTY
func (r *SSHRunner) GetPTY(cols, rows int, initialPath string) (io.WriteCloser, io.Reader, *ssh.Session, error) {
	session, err := r.Client.NewSession()
	if err != nil {
		return nil, nil, nil, err
	}

	// Set up terminal modes
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,     // enable echoing
		ssh.TTY_OP_ISPEED: 14400, // input speed = 14.4kbaud
		ssh.TTY_OP_OSPEED: 14400, // output speed = 14.4kbaud
	}

	// Request pseudo terminal
	if err := session.RequestPty("xterm-256color", rows, cols, modes); err != nil {
		session.Close()
		return nil, nil, nil, err
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		session.Close()
		return nil, nil, nil, err
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		return nil, nil, nil, err
	}

	// Start shell with initial path if provided
	if initialPath != "" && initialPath != "." {
		// Use a shell command to change directory and then start an interactive shell
		// We use 'exec bash' to replace the initial command with a real interactive shell
		cmd := fmt.Sprintf("cd %q && exec /bin/bash", initialPath)
		if err := session.Start(cmd); err != nil {
			// Fallback to default shell if bash fails
			if err := session.Shell(); err != nil {
				session.Close()
				return nil, nil, nil, err
			}
		}
	} else {
		if err := session.Shell(); err != nil {
			session.Close()
			return nil, nil, nil, err
		}
	}

	return stdin, stdout, session, nil
}

// GetMetrics fetches CPU, RAM and Disk usage from the remote server
func (r *SSHRunner) GetMetrics() (SystemMetrics, error) {
	// CPU Usage
	cpuCmd := "top -bn1 | grep 'Cpu(s)' | awk '{print 100 - $8}'"
	cpu, _ := r.Execute(cpuCmd)

	// RAM Usage (Percentage)
	ramCmd := "free | grep Mem | awk '{print ($3/$2)*100}'"
	ram, _ := r.Execute(ramCmd)

	// Disk Usage (Percentage of root)
	diskCmd := "df -h / | awk 'NR==2 {print $5}' | sed 's/%//'"
	disk, _ := r.Execute(diskCmd)

	return SystemMetrics{
		CPU:  strings.TrimSpace(cpu) + "%",
		RAM:  strings.TrimSpace(ram) + "%",
		Disk: strings.TrimSpace(disk) + "%",
	}, nil
}

func (r *SSHRunner) Close() {
	if r.SFTPClient != nil {
		r.SFTPClient.Close()
	}
	if r.Client != nil {
		r.Client.Close()
	}
}

// File Manager Methods

func (r *SSHRunner) ListDir(path string) ([]os.FileInfo, error) {
	if path == "" || path == "~" {
		path = "."
	}
	return r.SFTPClient.ReadDir(path)
}

func (r *SSHRunner) ReadFile(path string) ([]byte, error) {
	file, err := r.SFTPClient.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(file)
}

func (r *SSHRunner) WriteFile(path string, data []byte) error {
	file, err := r.SFTPClient.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(data)
	return err
}

func (r *SSHRunner) DeleteFile(path string) error {
	session, err := r.Client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()
	// Use absolute or correctly relative path via raw session to avoid CWD side effects from Execute()
	return session.Run(fmt.Sprintf("rm -rf \"%s\"", path))
}

func (r *SSHRunner) Mkdir(path string) error {
	return r.SFTPClient.MkdirAll(path)
}

func (r *SSHRunner) Rename(oldPath, newPath string) error {
	return r.SFTPClient.Rename(oldPath, newPath)
}
