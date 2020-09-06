package main

import (
	"errors"
	"fmt"
	"github.com/Unknwon/goconfig"
	"github.com/olekukonko/tablewriter"
	"github.com/shirou/gopsutil/process"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)


var (
	Agents map[string]*Agent
    CurDir string
	Logger *zap.SugaredLogger
)

// agent实例结构体
type Agent struct {
	ConfigPath string
	InputFilter string
	OutputFilter string
	LogPath string
	PidFile string
	BasePath string
}

func main() {
	// 初始化配置
	CurDir, _ = GetCurrentPath()
	InitLogger()
	InitConfig()

	var rootCmd = &cobra.Command{Use: "agentctl"}

	var cmdList = &cobra.Command{
		Use:   "list",
		Short: "显示所有Agent的启动配置项",
		Long: `打印所有Agent的启动配置项.

示例：
Agent启动配置列表如下:
+----------+---------------------------------+--------------------------------+-----------------+----------+-------------------+-------------------+
|   名称   |            项目路径             |            配置文件            |    输入插件     | 输出插件 |     日志文件      |      PID文件      |
+----------+---------------------------------+--------------------------------+-----------------+----------+-------------------+-------------------+
| example1 | /Users/dongxiaoyi/Go/src/agent/ | configs/monitor.example.1.conf | net:disk:diskio | kafka    | logs/example1.log | pids/example1.pid |
| example2 | /Users/dongxiaoyi/Go/src/agent/ | configs/monitor.example.2.conf | net:disk        | kafka    | logs/example2.log | pids/example2.pid |
+----------+---------------------------------+--------------------------------+-----------------+----------+-------------------+-------------------+

`,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("Agent启动配置列表如下:")
			table := tablewriter.NewWriter(os.Stdout)
			table.SetHeader([]string{"名称", "项目路径", "配置文件", "输入插件", "输出插件", "日志文件", "pid文件"})
			for k, v := range Agents {
				table.Append([]string{k, v.BasePath , "configs/"+v.ConfigPath, v.InputFilter, v.OutputFilter, "logs/"+v.LogPath, "pids/"+v.PidFile})
			}

			table.Render()
		},
	}

	var cmdStart = &cobra.Command{
		Use:   "start [all | 具体某一个worker]",
		Short: "启动Agent.",
		Long: `启动Agent，具体启动配置见agent.conf.
`,
		Run: func(cmd *cobra.Command, args []string) {
			Start(args)
		},
	}

	var cmdStatus = &cobra.Command{
		Use:   "status",
		Short: "检查Agent的运行状态.",
		Long: `检查Agent的运行状态.
示例：
Agent运行状态如下:
+----------+------+----------+
|   名称   | PID  | 运行状态 |
+----------+------+----------+
| example1 | 5781 | 正常     |
| example2 | 5783 | 正常     |
+----------+------+----------+
`,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("Agent运行状态如下:")
			table := tablewriter.NewWriter(os.Stdout)
			table.SetHeader([]string{"名称", "PID", "运行状态"})
			for k, agent := range Agents {
				pidFile := filepath.Join(agent.BasePath, "pids/"+agent.PidFile)
				if PathExist(pidFile) {
					pidStr := strings.Trim(ReadFile(pidFile), "\n")
					pid, err := strconv.ParseInt(pidStr,10,32)
					if err != nil {
						Logger.Error(err)
						break
					}
					if ProcessCheck(int32(pid)) {
						table.Append([]string{k, pidStr, "正常"})
					} else {
						table.Append([]string{k, "-", "掉线"})
					}
				} else {
					table.Append([]string{k, "-", "掉线"})
				}
			}
			table.Render()
		},
	}

	var cmdStop = &cobra.Command{
		Use:   "stop [all | 某一个agent]",
		Short: "关闭所有Agent， 或某一个Agent.",
		Long: `关闭所有Agent， 或某一个Agent..
`,
		Run: func(cmd *cobra.Command, args []string) {
			Stop(args)
		},
	}

	var cmdRestart = &cobra.Command{
		Use:   "restart [all | 某一个agent]",
		Short: "重启所有Agent， 或某一个Agent.",
		Long: `重启所有Agent， 或某一个Agent..
`,
		Run: func(cmd *cobra.Command, args []string) {
			Stop(args)
			time.Sleep(1000000000)
			Start(args)
		},
	}

	rootCmd.AddCommand(cmdList)
	rootCmd.AddCommand(cmdStart)
	rootCmd.AddCommand(cmdStatus)
	rootCmd.AddCommand(cmdStop)
	rootCmd.AddCommand(cmdRestart)
	err := rootCmd.Execute()
	if err != nil {
		Logger.Error(err)
	}

}

// 初始化配置文件
func InitConfig() {
	cfg := ParserConfig()
	Agents = make(map[string]*Agent)

	// 遍历配置文件的section生成Agent实例结构
	allWorker := cfg.GetSectionList()
	for _, worker := range allWorker {
		name := strings.Split(worker, ":")[1]
		Agents[name] = &Agent{
			BasePath: CurDir,
			ConfigPath: cfg.MustValue(worker, "config_path"),
			InputFilter: cfg.MustValue(worker, "input_filter"),
			OutputFilter: cfg.MustValue(worker, "output_filter"),
			LogPath: cfg.MustValue(worker, "log_path"),
			PidFile: cfg.MustValue(worker, "pid_file"),
		}
	}
}


// 获取指令所在目录的绝对路径
func GetCurrentPath() (string, error) {
	file, err := exec.LookPath(os.Args[0])
	if err != nil {
		return "", err
	}
	path, err := filepath.Abs(file)
	if err != nil {
		return "", err
	}
	i := strings.LastIndex(path, "/")
	if i < 0 {
		i = strings.LastIndex(path, "\\")
	}
	if i < 0 {
		return "", errors.New(`error: Can't find "/" or "\".`)
	}
	return string(path[0 : i+1]), nil
}


// 解包配置文件
func ParserConfig() *goconfig.ConfigFile {
	agentConfFile := filepath.Join(CurDir, "agent.conf")
	agentConfig, err := goconfig.LoadConfigFile(agentConfFile)
	if err != nil {
		fmt.Println("Get config file error")
		os.Exit(-1)
	}
	return agentConfig
}


// 日志器
func LogLevel() map[string]zapcore.Level {
	level := make(map[string]zapcore.Level)
	level["debug"] = zap.DebugLevel
	level["info"] = zap.InfoLevel
	level["warn"] = zap.WarnLevel
	level["error"] = zap.ErrorLevel
	level["dpanic"] = zap.DPanicLevel
	level["panic"] = zap.PanicLevel
	level["fatal"] = zap.FatalLevel
	return level
}

// 初始化日志
func InitLogger() {
	logLevelOpt := "DEBUG" // 日志级别
	levelMap := LogLevel()
	logLevel, _ := levelMap[logLevelOpt]
	atomicLevel := zap.NewAtomicLevelAt(logLevel)

	encodingConfig := zapcore.EncoderConfig{
		TimeKey: "Time",
		LevelKey: "Level",
		NameKey: "Log",
		CallerKey: "Celler",
		MessageKey: "Message",
		StacktraceKey: "Stacktrace",
		LineEnding: zapcore.DefaultLineEnding,
		EncodeLevel: zapcore.LowercaseLevelEncoder,
		EncodeTime: func(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
			enc.AppendString(t.Format("[2006-01-02 15:04:05]"))
		},
		EncodeDuration: zapcore.SecondsDurationEncoder,
		EncodeCaller: zapcore.FullCallerEncoder,
	}
	var outPath []string
	var errPath []string
	outPath = append(outPath, "stdout")
	errPath = append(outPath, "stderr")

	logCfg := zap.Config{
		Level: atomicLevel,
		Development: true,
		DisableCaller: true,
		DisableStacktrace: true,
		Encoding:"console",
		EncoderConfig: encodingConfig,
		// InitialFields: map[string]interface{}{filedKey: fieldValue},
		OutputPaths: outPath,
		ErrorOutputPaths: errPath,
	}

	logger, _ := logCfg.Build()
	Logger = logger.Sugar()
}


func ProcessCheck(pid int32) bool {
	isExist, err := process.PidExists(pid)
	if err != nil {
		Logger.Panic(err)
	}
	return isExist
}


func ReadFile(file string) string {
	bytes,err := ioutil.ReadFile(file)
	if err != nil {
		Logger.Fatal(err)
	}
	return string(bytes)
}


func PathExist(_path string) bool {
	_, err := os.Stat(_path)
	if err != nil && os.IsNotExist(err) {
		return false
	}
	return true
}

func StopPid(pid int32, name string) {
	p, _ := process.NewProcess(pid)
	if !ProcessCheck(pid) {
		Logger.Warnf("Agent [%s] 进程不存在！", name)
	} else {
		err := p.Kill()
		if err != nil {
			Logger.Error(err)
		} else {
			Logger.Infof("Agent [%s] 已停止！", name)
		}
	}
}


func Start(args []string) {
	if len(args) <=0 || args[0] == "all" {
		// 启动所有节点
		for name, agent := range Agents {
			exePath := filepath.Join(agent.BasePath, "lib/agent")
			cfgPath := filepath.Join(agent.BasePath, "configs/"+agent.ConfigPath)
			inputFilter := agent.InputFilter
			outputFilter := agent.OutputFilter
			pidFile := filepath.Join(agent.BasePath, "pids/"+agent.PidFile)
			logPath := filepath.Join(agent.BasePath,"logs/"+ agent.LogPath)

			// 首先检查进程状态，不存在则往下进行
			if PathExist(pidFile) {
				pidStr := strings.Trim(ReadFile(pidFile), "\n")
				pid, err := strconv.ParseInt(pidStr,10,32)
				if err != nil {
					Logger.Error(err)
					break
				}
				if ProcessCheck(int32(pid)) {
					Logger.Errorf("Agent [%s] 正在运行... ...", name)
					continue
				}
			}

			cmdStr := "nohup " + exePath + " --config " + cfgPath + " --input-filter " + inputFilter + " --output-filter " + outputFilter + " --pidfile " + pidFile + " >>" + logPath +" 2>&1 &"
			cmd := exec.Command("sh", "-c", cmdStr)
			stdout, err4 := cmd.StdoutPipe()
			cmd.Stderr = cmd.Stdout
			if err4 != nil {
				Logger.Error(err4)
				break
			}

			if err := cmd.Start(); err != nil {
				Logger.Error(err)
				break
			}

			for {
				tmp := make([]byte, 1024)
				_, err5 := stdout.Read(tmp)
				if err5 != nil {
					break
				}
			}
			err := cmd.Wait()
			if err != nil {
				Logger.Error(err)
				break
			}
			Logger.Infof("Agent [%s] 启动成功！", name)
		}
	} else {
		// 启动部分节点
		for _, name := range args {
			agent, bool := Agents[name]
			if !bool {
				Logger.Warnf("Agent [%s] 不存在，请检查输入！", name)
			} else {
				exePath := filepath.Join(agent.BasePath, "lib/agent")
				cfgPath := filepath.Join(agent.BasePath, "configs/"+agent.ConfigPath)
				inputFilter := agent.InputFilter
				outputFilter := agent.OutputFilter
				pidFile := filepath.Join(agent.BasePath, "pids/"+agent.PidFile)
				logPath := filepath.Join(agent.BasePath, "logs/"+agent.LogPath)

				// 首先检查进程状态，不存在则往下进行
				if PathExist(pidFile) {
					pidStr := strings.Trim(ReadFile(pidFile), "\n")
					pid, err := strconv.ParseInt(pidStr,10,32)
					if err != nil {
						Logger.Error(err)
						break
					}
					if ProcessCheck(int32(pid)) {
						Logger.Errorf("Agent [%s] 正在运行... ...", name)
						continue
					}
				}

				cmdStr := "nohup " + exePath + " --config " + cfgPath + " --input-filter " + inputFilter + " --output-filter " + outputFilter + " --pidfile " + pidFile + " >>" + logPath +" 2>&1 &"
				cmd := exec.Command("sh", "-c", cmdStr)
				stdout, err4 := cmd.StdoutPipe()
				cmd.Stderr = cmd.Stdout
				if err4 != nil {
					Logger.Error(err4)
					break
				}

				if err := cmd.Start(); err != nil {
					Logger.Error(err)
					break
				}

				for {
					tmp := make([]byte, 1024)
					_, err5 := stdout.Read(tmp)
					if err5 != nil {
						break
					}
				}
				err := cmd.Wait()
				if err != nil {
					Logger.Error(err)
					break
				}
				Logger.Infof("Agent [%s] 启动成功！", name)
			}
		}
	}
}


func Stop(args []string) {
	if len(args) <= 0 || args[0] == "all" {
		// 关闭所有agent
		for name, agent := range Agents {
			pidFile := filepath.Join(agent.BasePath, "pids/"+agent.PidFile)
			if PathExist(pidFile) {
				pidStr := strings.Trim(ReadFile(pidFile), "\n")
				pid, err := strconv.ParseInt(pidStr, 10, 32)
				if err != nil {
					Logger.Error(err)
				} else {
					StopPid(int32(pid), name)
				}
			} else {
				Logger.Warnf("Agent [%s] 进程不存在！", name)
			}
		}
	} else {
		// 关闭部分Agent
		for _, name := range args {
			agent, bool := Agents[name]
			if !bool {
				Logger.Warnf("Agent [%s] 不存在，请检查输入！", name)
			} else {
				pidFile := filepath.Join(agent.BasePath, "pids/"+agent.PidFile)
				if PathExist(pidFile) {
					pidStr := strings.Trim(ReadFile(pidFile), "\n")
					pid, err := strconv.ParseInt(pidStr, 10, 32)
					if err != nil {
						Logger.Error(err)
					} else {
						StopPid(int32(pid), name)
					}
				} else {
					Logger.Warnf("Agent [%s] 进程不存在！", name)
				}
			}
		}
	}
}

