package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"github.com/codegangsta/cli"
	"net/smtp"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

var (
	defaultExitCodes    = cli.IntSlice([]int{0})
	defaultMailReceiver = cli.StringSlice([]string{})
)

func main() {
	app := cli.NewApp()
	app.Name = "go-mini-supervisor"
	app.Usage = "supervisor single program"
	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:  "process_name",
			Value: "",
			Usage: "The name of the process. If this is unset, the name will be set as the basename of the command file.",
		},
		&cli.StringFlag{
			Name:  "stdout_logfile",
			Value: "",
			Usage: "Put process stdout output in this file. If this argument is unset or set to an empty string, the log file will not be created.",
		},
		&cli.StringFlag{
			Name:  "stdout_logfile_maxbytes",
			Value: "50MB",
			Usage: "The maximum number of bytes that may be consumed by \"stdout_logfile\" before it is rotated(suffix multipliers like “KB”, “MB”, and “GB” can be used in the value). Set this value to 0 to indicate an unlimited log size.",
		},
		&cli.IntFlag{
			Name:  "stdout_logfile_backups",
			Value: 10,
			Usage: "The number of \"stdout_logfile\" backups to keep around resulting from process stdout log file rotation. If set to 0, no backups will be kept.",
		},
		&cli.StringFlag{
			Name:  "stderr_logfile",
			Value: "",
			Usage: "Put process stderr output in this file. If this argument is unset or set to an empty string, the log file will not be created.",
		},
		&cli.StringFlag{
			Name:  "stderr_logfile_maxbytes",
			Value: "50MB",
			Usage: "The maximum number of bytes that may be consumed by \"stderr_logfile\" before it is rotated(suffix multipliers like “KB”, “MB”, and “GB” can be used in the value). Set this value to 0 to indicate an unlimited log size.",
		},
		&cli.IntFlag{
			Name:  "stderr_logfile_backups",
			Value: 10,
			Usage: "The number of \"stderr_logfile\" backups to keep around resulting from process stdout log file rotation. If set to 0, no backups will be kept.",
		},
		&cli.StringFlag{
			Name:  "autorestart",
			Value: "unexpected",
			Usage: "Specifies if mini-supervisor should automatically restart a process after it exits. May be one of \"false\", \"unexpected\", or \"true\". If false, the process will not be autorestarted. If unexpected, the process will be restarted when the program exits with an exit code that is not one of the exit codes specified by \"exitcodes\" argument. If true, the process will be unconditionally restarted when it exits, without regard to its exit code.",
		},
		&cli.IntSliceFlag{
			Name:  "exitcods",
			Value: &defaultExitCodes,
			Usage: "The list of expected exit codes for this program used with \"autorestart\" argument.",
		},
		&cli.IntFlag{
			Name:  "startretries",
			Value: 3,
			Usage: "The number of serial failure attempts that mini-supervisor \"autorestart\" the process.",
		},
		&cli.IntFlag{
			Name:  "startsecs",
			Value: 1,
			Usage: "The total number of seconds which the program need to stay running after a startup to consider the start successful.",
		},
		&cli.BoolFlag{
			Name:  "mail_alert",
			Usage: "Specifies if mini-supervisor should send a notification E-Mail after the process unexpceted exit.",
		},
		&cli.StringFlag{
			Name:  "mail_server",
			Value: "",
			Usage: "The SMTP server address for sending unexpected exit notification E-Mail.",
		},
		&cli.StringFlag{
			Name:  "mail_username",
			Value: "",
			Usage: "The username of the E-Mail account",
		},
		&cli.StringFlag{
			Name:  "mail_password",
			Value: "",
			Usage: "The password of the E-Mail account",
		},
		&cli.StringFlag{
			Name:  "mail_sender",
			Value: "",
			Usage: "The sender of the notification E-Mail. The sender will be the same as \"mail_username\" while this argument is unset.",
		},
		&cli.StringSliceFlag{
			Name:  "mail_receivers",
			Value: &defaultMailReceiver,
			Usage: "The recievers of the notification E-Mail",
		},
		&cli.StringFlag{
			Name:  "mail_subject",
			Value: "$program_name unexpected exit",
			Usage: "The subject of the notification E-Mail. \"$program_name\" in the subject will be replace with the program name specified by the \"program_name\" argument.",
		},
	}
	app.Action = start
}

func exit(code int, format string, params ...interface{}) {
	fmt.Fprintf(os.Stderr, format, params...)
	os.Exit(code)
}

type exitStatus struct {
	ExitCode   int
	Unexpected bool
	StartTime  time.Time
	ExitTime   time.Time
	Retry      int
	Restart    bool
}

func start(ctx *cli.Context) {
	if len(ctx.Args()) == 0 {
		exit(1, "No program command specified.\n")
	}
	startDelay := time.Duration(ctx.GlobalInt("startsecs")) * time.Second
	startRetries := ctx.GlobalInt("startretries")
	exitCodes := ctx.GlobalIntSlice("exitcodes")
	autoRestart := ctx.GlobalString("autorestart")
	if autoRestart != "true" && autoRestart != "false" && autoRestart != "unexpected" {
		exit(1, "Invalid autorestart argument")
	}
	retry := 0
	exited := false
	for !exited {
		cmd := exec.Command(ctx.Args()[0], ctx.Args()[1:]...)
		if stdoutLogfile := ctx.GlobalString("stdout_logfile"); stdoutLogfile != "" {
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				exit(1, "fetch stdout pipe fail: %s\n", err.Error())
			}
			pipeout, err := NewPipeOut(stdoutLogfile,
				ctx.GlobalString("stdout_logfile_maxbytes"), ctx.GlobalInt("stdout_logfile_backups"))
			if err != nil {
				exit(1, "open stdout logfile fail: %s\n", err.Error())
			}
			go pipeout.Pipe(stdout)
		}
		if stderrLogfile := ctx.GlobalString("stderr_logfile"); stderrLogfile != "" {
			stderr, err := cmd.StdoutPipe()
			if err != nil {
				exit(1, "fetch stderr pipe fail: %s\n", err.Error())
			}
			pipeout, err := NewPipeOut(stderrLogfile,
				ctx.GlobalString("stderr_logfile_maxbytes"), ctx.GlobalInt("stderr_logfile_backups"))
			if err != nil {
				exit(1, "open stderr logfile fail: %s\n", err.Error())
			}
			go pipeout.Pipe(stderr)
		}
		startTime := time.Now()
		if err := cmd.Start(); err != nil {
			exit(1, "start program fail: %s\n", err.Error())
		}
		exitCode := 0
		err := cmd.Wait()
		exitTime := time.Now()
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
					exitCode = int(status)
				}
			}
		}
		status := exitStatus{
			Unexpected: false,
			ExitCode:   exitCode,
			StartTime:  startTime,
			ExitTime:   exitTime,
			Retry:      retry,
		}
		if len(exitCodes) == 0 {
			status.Unexpected = true
			for _, code := range exitCodes {
				if exitCode == code {
					status.Unexpected = false
					break
				}
			}
		}
		if autoRestart == "true" || status.Unexpected && autoRestart == "unexpected" {
			if exitTime.Sub(startTime) < startDelay {
				retry++
			} else {
				retry = 0
			}
			if retry >= startRetries {
				exited = true
			}
		} else {
			exited = true
		}
		status.Restart = !exited
		if status.Unexpected {
			go alertUnexpectedExit(ctx, status)
		}
	}
}

func alertUnexpectedExit(ctx *cli.Context, status exitStatus) {
	if !ctx.GlobalBool("mail_alert") {
		return
	}
	programName := ctx.GlobalString("program_name")
	if programName == "" {
		programName = filepath.Base(ctx.Args()[0])
	}
	subject := ctx.GlobalString("mail_subject")
	subject = strings.Replace(subject, "$program_name", programName, -1)
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "From: %s\r\nTo: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=UTF-8\r\n",
		ctx.GlobalString("mail_sender"),
		strings.Join(ctx.GlobalStringSlice("mail_receivers"), ","),
		"=?utf-8?B?"+base64.StdEncoding.EncodeToString([]byte(subject))+"?=")
	fmt.Fprintf(&buf, "ExitCode: %d\nStartTime: %s\nExitTime: %s\nRetry: %d\nRestart: %v\n",
		status.ExitCode, status.StartTime.Format(time.RFC1123Z),
		status.ExitTime.Format(time.RFC1123Z), status.Retry, status.Restart)
	auth := smtp.PlainAuth(ctx.GlobalString("mail_username"),
		ctx.GlobalString("mail_username"), ctx.GlobalString("mail_password"),
		strings.Split(ctx.GlobalString("mail_server"), ":")[0])
	if err := smtp.SendMail(ctx.GlobalString("mail_server"), auth, ctx.GlobalString("mail_sender"),
		[]string(ctx.GlobalStringSlice("mail_receivers")), buf.Bytes()); err != nil {
		fmt.Fprintf(os.Stderr, "[mini-supervisor]send process unexpected exit notification mail fail: %s", err.Error())
	} else {
		fmt.Fprintf(os.Stderr, "[mini-supervisor]send process unexpected exit notification mail success")
	}
}
