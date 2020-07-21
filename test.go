package main

import (
	"encoding/json"
	"fmt"
	"github.com/fsnotify/fsnotify"
	"github.com/fvbock/endless"
	"github.com/gin-gonic/gin"
	"log"
	"os"
	"os/exec"
	"strconv"
	"syscall"

	"io/ioutil"
	"sync"
	"time"
)

type confModel struct {
	HttpPort    string `json:"HttpPort"`
	Ver         string `json:"Ver"`
	DelaySecond int    `json:"DelaySecond"`
	BinName     string `json:"BinName"`
}

var restartChan = make(chan bool)
var watchPath = "test_watch.conf"
var wait sync.WaitGroup

//当前版本
var curVer string = "init"

//当前进程的pid
var curPid string

func main() {
	simpleLog("启动 main")

	wait.Add(3)
	go watch()
	go restartWeb()
	go web()

	wait.Wait()
}

//监听文件改变
func watch() {
	simpleLog("启动 watch")
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		simpleLog(err)
	}
	defer watcher.Close()

	go func() {
		for {
			select {
			case event := <-watcher.Events:
				simpleLog("监听到事件event:", event, time.Now())
				//if event.Op&fsnotify.Write == fsnotify.Write {
				//	simpleLog("检测到文件修改")
				//	restartChan <- true
				//}

				//正式环境用boss推送，检测到的行为是删除
				if event.Op&fsnotify.Remove == fsnotify.Remove {
					simpleLog("检测到文件被删除")
					restartChan <- true
				}
			case <-watcher.Errors:
				//fmt.Println("error:", err)
			}
		}
	}()

	err = watcher.Add(watchPath)
	if err != nil {
		simpleLog(err)
		log.Fatal(err)
	}

	<-make(chan bool)

	//https://blog.csdn.net/weixin_33912453/article/details/86357935
}

//开始重启
func restartWeb() {
	simpleLog("启动 restartWeb")
	for {
		<-restartChan
		simpleLog("收到通知 准备 restartWeb")

		m := loadConf()
		if m == nil {
			simpleLog("conf 解析失败")
		} else {
			curVer = m.Ver
			binName := m.BinName

			//延时x秒后再执行重启，以防有的文件还没上传完成
			time.Sleep(time.Duration(m.DelaySecond) * time.Second)

			curPid = strconv.Itoa(syscall.Getpid())
			simpleLog("获取最新进程pid", curPid)

			//给文件授权
			simpleLog("chmod 777", binName)
			exec.Command("chmod", "777", binName).Run()

			//通知当前进程关闭
			simpleLog("kill -1", curPid)
			exec.Command("kill", "-1", curPid).Run()

			simpleLog("更新完成", curPid, binName)
		}
	}
}

//启动web
func web() {
	g := gin.New()

	//输出pid和版本信息
	g.GET("/hello", func(c *gin.Context) {
		pid := strconv.Itoa(syscall.Getpid())
		simpleLog("/hello", pid)
		c.JSON(200, gin.H{"message": "Hello Gin new2!", "ver": curVer, "pid": pid})
	})

	g.GET("/filelog", func(c *gin.Context) {
		//读日志文件
		bytes, err := ioutil.ReadFile("log.txt")
		if err != nil {
			c.String(200, "打开失败")
		} else {
			var text string = string(bytes)
			c.String(200, text)
		}
	})

	m := loadConf()
	curVer = m.Ver
	s := endless.NewServer(":"+m.HttpPort, g)
	err := s.ListenAndServe()
	if err != nil {
		simpleLog("server err: %v", err)
	}

	simpleLog("Server on " + m.HttpPort + " stopped")

	os.Exit(0)
}

//读取配置
func loadConf() *confModel {
	c, err := ioutil.ReadFile(watchPath)
	if err != nil {
		simpleLog(watchPath, "读取失败 error:", err)
		return nil
	}
	var conf confModel
	err = json.Unmarshal(c, &conf)
	if err != nil {
		simpleLog("conf 解析失败", err)
		return nil
	}
	return &conf
}

//简易日志
func simpleLog(a ...interface{}) {
	fmt.Println(a)
	msg := fmt.Sprintln(a)

	msg = time.Now().Format("2006-1-2 15:04:05") + "| " + msg

	//打开文件，读取文件  os.O_CREATE不存在会创建 os.O_APPEND写文件从末尾追加
	f3, err := os.OpenFile("log.txt", os.O_CREATE|os.O_WRONLY|os.O_APPEND, os.ModePerm)
	if err != nil {
		fmt.Println("打开文件错误", err)
	} else {
		defer f3.Close() //延迟到最后关闭
		//创建切片接收数据
		s1 := make([]byte, 1024)
		f3.Read(s1)

		//写数据
		f3.WriteString(msg)
	}
}
