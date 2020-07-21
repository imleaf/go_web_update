# go_web_update
endless+gin+fsnotify实现goweb自动平滑重启

## 前言
本文阐述如何使用endless+fsontify实现linux服务器上的热更新。原以为站点更新会像.net、java等那么方便，直接上传更新文件就会自动重启看到最新效果，但在golang中，需要我们手动来实现。
## 常规部署
##### 步骤
go web在服务器上的部署步骤一般是打包成二进制文件部署在多台Linux服务器上，可搭配lvs、Nginx来做反向代理，实现负载均衡，保障站点的高可用。
##### 问题
linux上二进制文件启动后，无法直接替换，得有个手动重启的动作，这个对更新来说就非常麻烦了，而且程序员可能还不允许接触服务器，得通过运维同学来协助，这明显不友好。
##### 现有解决方案
在网上浏览一番后，我总结了以下几种方式
1. **自动编译** 有一些框架提供了自动编译的功能，例如 gin、fresh、rizla。这些都需要go环境，原理是检测到code有变化，自动帮你做类似go bulid的事，这并不适合服务器，服务器上不会去装go环境这么麻烦的东西，仅适用本地开发环境。
2. **容器** 使用docker等容器来部署
3. **endless** 为了让服务不中断，优雅的进行重启，可以利用 endless 来替换标准库net/http的ListenAndServe

本文就阐述第3种方案的实践。

## 实现
##### 思路
站点的更新，首先要更新文件，接着要关闭原进程，重新启动。我们就用程序来帮我们实现这件事情，设定一个watch.conf的监控文件，应用fsontify来监听文件的变更，如果检测到变更，开始执行命令，授权新文件的权限：chmod 777 [binname]，接着通知当前进程：kill -1 [pid]。最后endless发挥作用，会把程序重新启动，并且使用的是原来的请求上下文。笼统的概括完之后，下面讲解下关键部分。

##### watch.conf
在项目下新建test.watch.conf，内容如下
```
{
    "HttpPort": "8080",
    "Ver": "1",
    "DelaySecond":  5,
    "BinName":  "test"
}
```

* **HttpPort** 是我们站点的监听端口
* **Ver** 当前版本
* **DelaySecond** 延迟多少秒启动，为了避免项目里有其他的资源没有上传成功，设定一个缓冲的时间，但一般就一个二进制文件要更新而已。
* **BinName** 你编译的二进制文件名，就是你go build后生成的。

##### fsontify监听watch.conf
```
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
```
监听到fsnotify.Write或者fsnotify.Remove，(看大家使用的上传文件方式,方式不同，事件可能不一样)，然后给通道restarChan发送消息，表示可以更新了。 

##### 执行命令
```
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
```
等待通道`restarChan`，当收到消息，表示可以开始执行命令了；`loadConf()`读取.conf文件的结构，序列化到struct；给新的二进制文件授权`exec.Command("chmod", "777", binName).Run()`;获取当前的进程PID，最后执行`exec.Command("kill", "-1", curPid).Run()`通知自己的进程，注意是-1，不是-9，有差别，-9就杀死进程，启动不起来的。

##### endless
```
    g := gin.New()

	//输出pid和版本信息
	g.GET("/hello", func(c *gin.Context) {
		pid := strconv.Itoa(syscall.Getpid())
		simpleLog("/hello", pid)
		c.JSON(200, gin.H{"message": "Hello Gin new2!", "ver": curVer, "pid": pid})
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
```
选用的web框架是gin，原先用的是iris，发现不行。当然不用web框架，用`mux.NewRouter()`也可以的。

## 总结
linux上的站点目录如下，一个test go的二进制文件，一个.conf监控文件。

![image.png](https://image-static.segmentfault.com/942/905/942905324-5f1659a480b5d_articlex)

变更watch.conf后记录的日志

![image.png](https://image-static.segmentfault.com/213/702/2137028625-5f165b2c4e684_articlex)

如果有任何不对的或者建议，欢迎指出，共同改进。

