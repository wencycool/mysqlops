package instance
//获取当前MySQL运行实例信息

import (
	"fmt"
	"github.com/pkg/errors"
	"log"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

func init()  {
	log.SetFlags(log.Lshortfile|log.LstdFlags)
}



func caller(i int) string {
	if _,f,l,ok := runtime.Caller(i);ok {
		return fmt.Sprintf("file:%s,line:%d",f,l)
	}else{
		return "cannot print file line"
	}
}


type MySQLInstance struct {
	//UserName string
	User user.User
	PID int
	MysqldPath string
	Parms map[string]string `json:"-"`
	NetStat NetStat
	PidInfo string `json:"-"`//打印进程相关的信息
	MycnfPathList []string `json:"-"`//打印可能查找的my.cnf文件路径
	Mycnf string //my.cnf文件路径
	ShortVersion string //version的版本号
	Version string `json:"-"`//完整的version信息
	err error
}

type NetStat struct {
	PID int `json:"-"`
	Stat string
	ProjName string
	Port int
	SocketFile string //如果启用了本地sock协议
	err error
}

func getMySQLNetStat() (map[int]*NetStat,error)  {
	var (
		cmd *exec.Cmd
		err error
		outputBytes []byte
		outputString string
		netStatMap map[int]*NetStat //PID netstat
	)
	netStatMap = make(map[int]*NetStat,0)
	shell_cmd_text_xnlp := "netstat -xnlp" //获取本地socket文件
	//将socket协议解析放入map中,key=PID,val =Path
	sockMap := make(map[int]string,0)
	cmd = exec.Command("sh","-c",shell_cmd_text_xnlp)
	outputBytes,err = cmd.CombinedOutput()
	if err != nil {
		return nil,errors.Wrap(err,caller(1))
	}
	outputString = string(outputBytes)
	for _,eachLine := range strings.Split(outputString,"\n") {
		if !(strings.Contains(eachLine, "mysqld") && strings.HasPrefix(eachLine,"unix")) {
			continue
		}
		numField := strings.Fields(eachLine[strings.Index(eachLine,"LISTENING"):])
		pid,_ := strconv.Atoi(strings.Split(numField[2],"/")[0])
		socketPath := numField[3]
		sockMap[pid] = socketPath
	}
	shell_cmd_text_tnlp := "netstat -tnlp" //获取程序port端口号地址
	cmd = exec.Command("sh","-c",shell_cmd_text_tnlp)
	outputBytes,err = cmd.CombinedOutput()
	if err != nil {
		return nil,errors.Wrap(err,caller(1))
	}
	outputString = string(outputBytes)
	for _,eachLine := range strings.Split(outputString,"\n") {
		if !(strings.Contains(eachLine, "mysqld") && strings.HasPrefix(eachLine,"tcp")) {
			continue
		}
		numField := strings.Fields(eachLine)
		netstat := new(NetStat)
		netstat.Port,_ = strconv.Atoi(strings.TrimSpace(numField[3][strings.LastIndex(numField[3],":")+1:]))
		netstat.Stat = numField[5]
		netstat.PID,_  = strconv.Atoi(strings.Split(numField[6],"/")[0])
		netstat.ProjName = strings.SplitN(numField[6],"/",2)[1]
		netstat.SocketFile = sockMap[netstat.PID]
		netStatMap[netstat.PID] = netstat
	}
	return netStatMap,nil
}

func GetMySQLInstances() ([]*MySQLInstance,error)  {
	var (
		shell_cmd_text string
		cmd *exec.Cmd
		err error
		outputBytes []byte
		outputString string
		mysqlInstances []*MySQLInstance
		netStatMap map[int]*NetStat
	)
	if netStatMap,err  = getMySQLNetStat();err != nil {
		return nil,errors.Wrap(err,caller(1))
	}
	shell_cmd_text = "ps -ef"
	cmd = exec.Command("sh","-c",shell_cmd_text)
	outputBytes,err = cmd.CombinedOutput()
	if err != nil {
		return nil,errors.Wrap(err,caller(1))
	}
	outputString = string(outputBytes)
	//my.cnf查找路径
	//--defaults-file
	//Default options are read from the following files in the given order:
	///etc/mysql/my.cnf /etc/my.cnf ~/.my.cnf

	mysqlInstances = make([]*MySQLInstance,0)
eachInstance:
	for _,eachLine := range strings.Split(outputString,"\n") {
		//判断是否mysqld进程
		if !strings.Contains(eachLine,"mysqld") {
			continue
		}
		numField := strings.Fields(eachLine)
		if len(numField) <8 || !strings.HasSuffix(numField[7],"mysqld") {
			continue
		}
		inst := new(MySQLInstance)
		inst.Parms = make(map[string]string,0)
		inst.PidInfo = eachLine

		isParm := false
		for i:=0;i<len(numField) ;i++ {
			switch {
			case strings.HasSuffix(numField[i],"mysqld"):
				username := numField[0]
				if user,err := user.Lookup(username);err != nil {
					inst.err = err
					continue eachInstance
				}else {
					inst.User = *user
				}
				inst.PID,_ = strconv.Atoi(numField[1])
				isParm = true
				inst.MysqldPath = numField[i]
			case isParm && strings.Contains(numField[i],"="):
				subNumField := strings.SplitN(numField[i],"=",2)
				var (
					key string
					val string
				)
				if len(subNumField) ==1 {
					key = strings.SplitN(numField[i],"=",2)[0]
					val = ""
				}else if len(subNumField) == 2 {
					key = strings.SplitN(numField[i],"=",2)[0]
					val = strings.SplitN(numField[i],"=",2)[1]
				}
				inst.Parms[key] = val
			}
		}
		if netStatMap[inst.PID] != nil {
			inst.NetStat = *netStatMap[inst.PID]
		}
		//添加版本信息
		//检查mysqld是否有效路径
		if finfo,err := os.Stat(inst.MysqldPath);err != nil {
			inst.err = err
		}else {
			if finfo.IsDir() {
				inst.err = errors.New(fmt.Sprintf("%s is directory!",inst.MysqldPath))
			}else {
				//获取版本信息
				get_version_cmd_text := fmt.Sprintf("%s --verbose --version",inst.MysqldPath)
				outputBytes,err := exec.Command("sh","-c",get_version_cmd_text).CombinedOutput()
				if err != nil {
					inst.err = err
				}else {
					inst.Version = strings.TrimSpace(string(outputBytes))
					numField := strings.Fields(inst.Version)
					if len(numField)>3 && regexp.MustCompile(`\d{1,2}\.\d{1,2}\.\d{1,2}`).MatchString(numField[2]) {
						inst.ShortVersion = numField[2]
					}else {
						inst.err = errors.New(fmt.Sprintf("%v field less than 3 or field 2 is not is version format",numField))
					}
				}
			}
		}
		//打印可能查找的my.cnf文件路径
		//先看是否在参数列表中存在
		if v,ok := inst.Parms["--defaults-file"];ok {
			inst.MycnfPathList = append(inst.MycnfPathList, v)
		}
		if inst.err == nil {
			get_mycnf_cmd_text := fmt.Sprintf("%s --verbose --help",inst.MysqldPath)
			outputBytes,err := exec.Command("sh","-c",get_mycnf_cmd_text).CombinedOutput()
			if err != nil {
				inst.err = err
			}else {
				isMycnfPathList := false
				for _,eachLine := range strings.Split(string(outputBytes),"\n") {
					if strings.HasPrefix(eachLine,"Default options are read from the following files in the given order") {
						isMycnfPathList = true
						continue
					}
					if isMycnfPathList {
						numField := strings.Fields(eachLine)
						for _,v := range numField {
							//如果是~开头，那么是启动用户的home目录
							if strings.HasPrefix(v,"~") {
								v = filepath.Join(inst.User.HomeDir,v[1:])
							}
							inst.MycnfPathList = append(inst.MycnfPathList, v)
						}
						isMycnfPathList = false
						break
					}
				}
			}
		}
		//确定my.cnf文件位置
		for _,eachPath := range inst.MycnfPathList {
			if finfo,err := os.Stat(eachPath);err == nil && !finfo.IsDir(){
				inst.Mycnf = eachPath
				break
			}
		}
		mysqlInstances  = append(mysqlInstances, inst)
	}
	return mysqlInstances,nil
}
