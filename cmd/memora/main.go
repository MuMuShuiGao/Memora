// Command memora 是服务端唯一入口（ADR-0006：一个静态二进制 + systemd）。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"golang.org/x/crypto/bcrypt"

	"memora/internal/api"
	"memora/internal/auth"
	"memora/internal/config"
	"memora/internal/report"
	"memora/internal/store"
)

// 打包时可用 -ldflags "-X main.version=..." 覆盖。
var version = "0.1.0"

const usage = `用法：memora <子命令>

  serve      起 HTTPS 服务，读 -config 指向的 KEY=VALUE 文件（可省略，改用环境变量）
  hashpass   从标准输入读一遍访问口令，输出 bcrypt 哈希，贴进配置即可
  version    打印版本
`

// bcrypt 只吃 72 字节，超长会直接报错而不是截断——口令在这里也一样拒绝，
// 否则配置里存的哈希和用户实际输入的口令不是同一个东西。
const maxPassphraseBytes = 72

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		if !errors.Is(err, errUsage) {
			fmt.Fprintln(os.Stderr, "错误：", err)
		}
		os.Exit(1)
	}
}

var errUsage = errors.New("usage")

func run(args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(stdout, usage)
		return errUsage
	}
	switch args[0] {
	case "serve":
		return serve(args[1:], stdout)
	case "hashpass":
		return hashPass(stdin, stdout)
	case "version":
		fmt.Fprintf(stdout, "memora %s\n", version)
		return nil
	case "-h", "--help", "help":
		fmt.Fprint(stdout, usage)
		return nil
	}
	return fmt.Errorf("未知子命令 %q（memora help 看用法）", args[0])
}

func serve(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(stdout)
	configPath := fs.String("config", "", "配置文件路径，KEY=VALUE")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}

	log := slog.New(slog.NewTextHandler(stdout, &slog.HandlerOptions{Level: levelFromEnv()}))

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer func() {
		if err := st.Close(); err != nil {
			log.Error("关闭数据库失败", "err", err.Error())
		}
	}()

	gate, err := auth.NewGate(cfg.PassphraseHash)
	if err != nil {
		return err
	}

	var completer report.Completer
	if c, err := report.NewCompleter(cfg.DeepSeekBaseURL, cfg.DeepSeekModel, cfg.DeepSeekAPIKey); err != nil {
		// 不阻断启动：记录和读报表照样能用，只是生成会明确报"未配置"。
		log.Warn("外部模型未配置，报表生成不可用", "err", err.Error())
	} else {
		completer = c
	}

	gen, err := report.NewGenerator(st, completer, cfg.LocalTZ, log)
	if err != nil {
		return err
	}

	srv := api.New(st, cfg, gate, gen, log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 进程内调度（ADR-0006）。它只查两次索引 + 状态，不做精确定时，
	// 所以错过某个时刻不影响结果，起来就能补上。
	sched := report.NewScheduler(gen, st, cfg.LocalTZ, log)
	go sched.Run(ctx)

	n, err := st.CountNotes(ctx)
	if err != nil {
		return err
	}
	log.Info("启动", "db", cfg.DBPath, "notes", n, "tz", cfg.LocalTZName, "maxChars", cfg.MaxNoteChars,
		"llm", completer != nil)

	if err := srv.Run(ctx); err != nil {
		return err
	}
	log.Info("已退出")
	return nil
}

func hashPass(stdin io.Reader, stdout io.Writer) error {
	raw, err := io.ReadAll(stdin)
	if err != nil {
		return fmt.Errorf("读口令失败: %w", err)
	}
	pass := strings.TrimRight(string(raw), "\r\n")
	if pass == "" {
		return errors.New("标准输入是空的，用法：printf %s '口令' | memora hashpass")
	}
	if len(pass) > maxPassphraseBytes {
		return fmt.Errorf("口令 %d 字节，超过 bcrypt 上限 %d", len(pass), maxPassphraseBytes)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("生成哈希失败: %w", err)
	}
	fmt.Fprintf(stdout, "%s\n", hash)
	fmt.Fprintln(stdout, "把它填进 MEMORA_PASS_HASH。")
	return nil
}

func levelFromEnv() slog.Level {
	if strings.EqualFold(os.Getenv("MEMORA_LOG_DEBUG"), "true") {
		return slog.LevelDebug
	}
	return slog.LevelInfo
}
