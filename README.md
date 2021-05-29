# appdeploy
简单的远程执行工具，可与集成编译系统整合使用，用于文件上传，远程执行shell命令  

# 证书设置  
支持私有证书，用于加密通讯  
服务器证书的名字为：  
ca.cert.pem  
server.cert.pem  
server.key.pem  

客户端证书的名字:   
ca.cert.pem  
client.cert.pem  
client.key.pem  

证书需要在同一个目录下  

# 命令格式  
## upload  
~~~
可以向服务器上传单个文件或者文件夹，命令格式如下所示：  
传单个文件：   
go_build_appdeploy.exe --certdir=F:/cert --host=wss://ip:port --cmd=upload --source="F:\dev\testproject\a.dat" --target="/work/testproject/a.dat"  
传目录:  
go_build_appdeploy.exe --certdir=F:/cert --host-wss://ip:port --cmd=upload --source="f:\dev\testproject" --target="/work/testproject"  
~~~

## shell  
~~~
用于执行没有回显的命令，比如启动某个进程。如果执行 nohup命令，请使用这个命令。  
go_build_appdeploy.exe --certdir=F:/cert --host-wss://ip:port --cmd=shell  --target="nohup /work/testproject/test &" --dir=/work/testproject --wait=true
~~~

## popen  
~~~
使用linux popen执行命令，实时返回命令执行输出，也就是接管了新进程的stderr,stdout   
go_build_appdeploy.exe --certdir=F:/cert --host-wss://ip:port --cmd=popen  --target="ls -l" --dir=/work/testproject
~~~

# 其他  
命令执行的返回值从服务器传给客户端作为客户端的退出码，集成编译系统可以捕获这个返回码。  
