package pkg

import (
	"fmt"
	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"
	log "github.com/sirupsen/logrus"
	"math"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

var (
	snapshotLen            = int32(65536)
	promiscuous            = true
	timeout                = pcap.BlockForever
	udIdAndFileMap         sync.Map
	fileAndIPPortMap       sync.Map
	ipPortTotalTrafficMap  sync.Map
	fileSizeMap            sync.Map
	ipPortAckSeqTrafficMap sync.Map
)

func BindUdIdAndFile(udId, file string) {
	udIdAndFileMap.Store(udId, file)
}

func GetDownloading(udId string) int {
	var fileSize, downloadSize int64

	//step1:根据udId获取文件
	iFileName, ok := udIdAndFileMap.Load(udId)
	if !ok {
		log.Warningf("未获取到udId(%s)对应的文件名称", udId)
		return 0
	}
	fileName, ok := iFileName.(string)
	if !ok {
		log.Warningf("udId(%s)对应的文件(%v)类型断言失败", udId, iFileName)
		return 0
	}
	if fileName == "" {
		log.Warningf("udId(%s)对应的文件(%v)名为空", udId, iFileName)
		return 0
	}

	//step1.1:根据文件名获取文件大小
	iFileSize, ok := fileSizeMap.Load(fileName)
	if !ok {
		log.Warningf("未获取到udId(%s)->文件(%s)的大小", udId, fileName)
		return 0
	}
	tempFileSize, ok := iFileSize.(int64)
	if !ok {
		log.Warningf("udId(%s)->文件(%v)所对应的IPPort(%v)类型断言失败", udId, fileName, iFileSize)
		return 0
	}
	fileSize = tempFileSize

	//step2:根据文件名获取ip:port
	iIPPort, ok := fileAndIPPortMap.Load(fileName)
	if !ok {
		log.Warningf("未获取到udId(%s)->文件(%s)的IP:Port", udId, fileName)
		return 0
	}
	ipPort, ok := iIPPort.(string)
	if !ok {
		log.Warningf("udId(%s)->文件(%v)所对应的IPPort(%v)类型断言失败", udId, fileName, iIPPort)
		return 0
	}

	//step3:根据ip:port获取流量
	iTraffic, ok := ipPortTotalTrafficMap.Load(ipPort)
	if !ok {
		log.Warningf("未获取到UdId(%s)->文件(%s)->IP:Port(%s)对应的下载流量", udId, fileName, ipPort)
		return 0
	}
	traffic, ok := iTraffic.(int64)
	if !ok {
		log.Warningf("UdId(%s)->文件(%s)->IPPort(%s)类型所对应的流量(%v)类型断言失败", udId, fileName, ipPort, iTraffic)
		return 0
	}

	//step4:流量统计
	downloadSize = traffic
	log.Infof("download size:%v,file size:%v", downloadSize, fileSize)
	if fileSize == 0 {
		return 0
	}

	return int(math.Min(float64(downloadSize)/float64(fileSize)*100, 100))
}

//TODO:网络流量抓包监控
func WireShark(watchPort uint16, deviceName string, filterRule string) {
	deviceIP, err := getDeviceIP(deviceName)
	if nil != err {
		return
	}
	log.Infof("Device(%s)对应的IP为:%s", deviceName, deviceIP)
	filter := getFilter(watchPort)
	handle, err := pcap.OpenLive(deviceName, snapshotLen, promiscuous, timeout)
	if err != nil {
		log.Error(err)
		return
	}
	if err := handle.SetBPFFilter(filter); err != nil {
		log.Error(err)
		return
	}
	defer handle.Close()

	packetSource := gopacket.NewPacketSource(handle, handle.LinkType())
	packetSource.NoCopy = true

	for packet := range packetSource.Packets() {
		if packet.NetworkLayer() == nil || packet.TransportLayer() == nil || packet.TransportLayer().LayerType() != layers.LayerTypeTCP {
			log.Info("unexpected packet")
			continue
		}
		var srcIP, srcPort, dstIP, dstPort string

		ipLayer := packet.Layer(layers.LayerTypeIPv4)
		if ipLayer != nil {
			ip, _ := ipLayer.(*layers.IPv4)
			srcIP = ip.SrcIP.String()
			dstIP = ip.DstIP.String()
		}

		tcpLayer := packet.Layer(layers.LayerTypeTCP)
		var seq, ack uint32
		if tcpLayer != nil {
			tcp, _ := tcpLayer.(*layers.TCP)
			srcPort = tcp.SrcPort.String()
			dstPort = tcp.DstPort.String()
			seq = tcp.Seq
			ack = tcp.Ack
		}

		//TODO:入口流量监控，数据包确认
		applicationLayer := packet.ApplicationLayer()
		if !strings.Contains(srcPort, strconv.Itoa(int(watchPort))) && dstIP != deviceIP {
			if iPacketTraffic, ok := ipPortAckSeqTrafficMap.Load(fmt.Sprintf("%v_%v_%v_%v", srcIP, srcPort, ack, seq)); ok {
				var packetTraffic, totalTraffic int64
				if packetTraffic, ok = iPacketTraffic.(int64); !ok {
					continue
				}
				if iTempTotalTraffic, ok := ipPortTotalTrafficMap.Load(srcIP + "_" + srcPort); ok {
					if tempTotalTraffic, ok := iTempTotalTraffic.(int64); ok {
						totalTraffic = tempTotalTraffic
					}
				}
				ipPortTotalTrafficMap.Store(srcIP+"_"+srcPort, totalTraffic+packetTraffic)
				fmt.Printf("ipPortTotalTrafficMap--->key:%s,traffic:%v\n", fmt.Sprintf("%v_%v_%v_%v", srcIP, srcPort, ack, seq), totalTraffic+packetTraffic)
			}

			if applicationLayer == nil {
				continue
			}
			//下载请求初始化
			inputPayloadStr := string(applicationLayer.Payload())
			log.Infof("request:%s", inputPayloadStr)
			if match, _ := regexp.MatchString(filterRule, inputPayloadStr); match {
				requests := strings.Split(inputPayloadStr, " ")
				if len(requests) < 2 {
					continue
				}
				u, err := url.Parse(requests[1])
				if nil != err {
					log.Error(err)
					continue
				}
				paths := strings.Split(u.Path, "/")
				fileName := paths[len(paths)-1]
				if "" == fileName {
					log.Errorf("未获取到文件名,%s", u.Path)
					continue
				}
				fileAndIPPortMap.Store(fileName, srcIP+"_"+srcPort)
				ipPortTotalTrafficMap.Store(srcIP+"_"+srcPort, int64(0))
			}
			continue
		}

		//TODO:出口流量统计，如何去噪
		if applicationLayer == nil || srcIP == deviceIP {
			continue
		}
		if _, ok := ipPortAckSeqTrafficMap.Load(fmt.Sprintf("%v_%v_%v_%v", dstIP, dstPort, seq, ack)); ok {
			continue
		}
		ipPortAckSeqTrafficMap.Store(fmt.Sprintf("%v_%v_%v_%v", dstIP, dstPort, seq, ack), int64(len(applicationLayer.Payload())))
		fmt.Printf("ipPortAckSeqTrafficMap--->key:%s,traffic:%v\n", fmt.Sprintf("%v_%v_%v_%v", dstIP, dstPort, seq, ack), int64(len(applicationLayer.Payload())))
	}
}

//TODO:定义过滤器
func getFilter(port uint16) string {
	filter := fmt.Sprintf("tcp and ((src port %v) or (dst port %v))", port, port)
	return filter
}

func SetFileSize(fileName string, fileSize int64) {
	fileSizeMap.Store(fileName, fileSize)
}

func getDeviceIP(deviceName string) (string, error) {
	ips := make(map[string]string)

	netInterfaces, err := net.Interfaces()
	if err != nil {
		log.Error(err)
		return "", err
	}

	for i := 0; i < len(netInterfaces); i++ {
		tempInterface := netInterfaces[i]
		if (tempInterface.Flags & net.FlagUp) != 0 {
			deviceInfo, err := net.InterfaceByName(tempInterface.Name)
			if err != nil {
				log.Error(err)
				continue
			}
			addresses, _ := netInterfaces[i].Addrs()
			for _, address := range addresses {
				if ipNet, ok := address.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
					if ipNet.IP.To4() != nil {
						ips[deviceInfo.Name] = ipNet.IP.String()
					}
				}
			}
		}
	}
	return ips[deviceName], nil
}

func RemoveDownloading(udid string) {
	//step1:根据udid获取文件
	iFileName, ok := udIdAndFileMap.Load(udid)
	if !ok {
		log.Warningf("RemoveDownloading:未获取到UdId(%s)对应的文件名称", udid)
		return
	}
	fileName, ok := iFileName.(string)
	if !ok {
		log.Warningf("RemoveDownloading:UdId(%s)对应的文件(%v)类型断言失败", udid, iFileName)
		return
	}
	if fileName == "" {
		log.Warningf("RemoveDownloading:UdId(%s)对应的文件(%v)名为空", udid, iFileName)
		return
	}

	//step2:根据文件名获取ip:port
	iIPPort, ok := fileAndIPPortMap.Load(fileName)
	if !ok {
		log.Warningf("RemoveDownloading:未获取到Udid(%s)->文件(%s)的IP:Port", udid, fileName)
		return
	}
	ipPort, ok := iIPPort.(string)
	if !ok {
		log.Warningf("RemoveDownloading:UdId(%s)->文件(%v)所对应的IPPort(%v)类型断言失败", udid, fileName, iIPPort)
		return
	}

	udIdAndFileMap.Delete(udid)

	fileAndIPPortMap.Delete(fileName)

	ipPortTotalTrafficMap.Delete(ipPort)
}
