# speedtest

[![Hits](https://hits.seeyoufarm.com/api/count/incr/badge.svg?url=https%3A%2F%2Fgithub.com%2Foneclickvirt%2Fspeedtest&count_bg=%232EFFF8&title_bg=%23555555&icon=&icon_color=%23E7E7E7&title=hits&edge_flat=false)](https://hits.seeyoufarm.com) [![Build and Release](https://github.com/oneclickvirt/speedtest/actions/workflows/main.yaml/badge.svg)](https://github.com/oneclickvirt/speedtest/actions/workflows/main.yaml)

就近节点测速模块

## 说明

- [x] 基于[speedtest.net-爬虫](https://github.com/spiritLHLS/speedtest.net-CN-ID)、[speedtest.cn-爬虫](https://github.com/spiritLHLS/speedtest.cn-CN-ID)的数据
- [x] 基于[speedtest-go](https://github.com/showwin/speedtest-go)二次开发，go原生实现就近测速无需使用shell命令
- [x] 主体逻辑借鉴了[ecsspeed](https://github.com/spiritLHLS/ecsspeed)
- [x] 使用shell命令使用```speedtest```进行测速

## TODO

- [ ] 国内服务器安装```speedtest-go```而不是```speedtest```进行测速
- [ ] 使用shell命令使用```speedtest-go```进行测速

## 下载speedtest或speedtest-go

```
curl https://raw.githubusercontent.com/oneclickvirt/speedtest/main/dspt.sh -sSf | bash
```

或

```
curl https://cdn.spiritlhl.net/https://raw.githubusercontent.com/oneclickvirt/speedtest/main/dspt.sh -sSf | bash
```

## 使用

下载及安装

```
curl https://raw.githubusercontent.com/oneclickvirt/speedtest/main/spt_install.sh -sSf | bash
```

或

```
curl https://cdn.spiritlhl.net/https://raw.githubusercontent.com/oneclickvirt/speedtest/main/spt_install.sh -sSf | bash
```

使用

```
spt
```

或

```
./spt
```

进行测试

无环境依赖，理论上适配所有系统和主流架构，更多架构请查看 https://github.com/oneclickvirt/speedtest/releases/tag/output

```
Usage of spt:
  -e    Enable logging
  -l string
        Language parameter (options: en, zh) (default "zh")
  -m string
        Test Method parameter (options: origin, speedtest, speedtest-go) (default "speedtest")
  -nearby
        Test only nearby servers
  -num int
        Number of test servers, default -1 not to limit (default -1)
  -opt string
        Operator parameter (options: cmcc, cu, ct, sg, tw, jp, hk, global) (default "global")
  -pf string
        Platform parameter (options: net, cn) (default "net")
  -s    Show head (default true)
  -v    Show version information
```

## 卸载

```
rm -rf /root/spt
rm -rf /usr/bin/spt
```

## 在Golang中使用

```
go get github.com/oneclickvirt/speedtest@latest
```
