package gn

import (
	"runtime"
	"runtime/debug"
	"sync"

	"context"
	"gn/config"
	"gn/glog"
	"gn/gnError"
	"gn/linker"

	"github.com/golang/protobuf/proto"
)

type IAppCmd interface {
	Run() error
	ReceiveCmdPack(pack IPack)
	AddCmdHandler(cmd string, handler HandlerFunc)
	Done()
}

func NewAppCmd(loger *glog.Glogger, nlinks linker.ILinker, detect *gnError.GnExceptionDetect) IAppCmd {
	return &AppCmd{
		cmdHandlers: make(map[string]HandlerFunc, 1<<4),
		inChan:      make(chan IPack, 1<<4),
		logger:      loger,
		links:       nlinks,
		isRuning:    false,
		exDetect:    detect,
	}
}

type AppCmd struct {
	cmdHandlers map[string]HandlerFunc
	inChan      chan IPack
	links       linker.ILinker
	logger      *glog.Glogger
	inCancal    context.CancelFunc
	handleMutex sync.RWMutex
	isRuning    bool
	exDetect    *gnError.GnExceptionDetect
}

func (ap *AppCmd) Run() error {
	// add handler
	if !ap.isRuning {
		ap.AddCmdHandler(config.CMD_PING, ap.PingHandler)
		ap.AddCmdHandler(config.CMD_MEM, ap.MemHandler)
		ctx, cancal := context.WithCancel(context.Background())
		ap.inCancal = cancal
		go ap.LoopCmdInChan(ctx)
		ap.isRuning = false
		return nil
	} else {
		ap.logger.Errorf(" appCmd  componentis aleady Runing ")
		return gnError.ErrAPPCMDMRuning
	}

}

func (ap *AppCmd) PingHandler(pack IPack) {
	if pack.GetRouter() == config.CMD_PING {
		respon := &config.CmdMsg{}
		if pack.GetAPP() != nil {
			respon.RunRoutineNum = int64(pack.GetAPP().GetRunRoutineNum())
		}
		pack.ResultProtoBuf(respon)
	}

}

func (ap *AppCmd) MemHandler(pack IPack) {
	if pack.GetRouter() == config.CMD_MEM {
		// get  mem
		memSet := &runtime.MemStats{}
		runtime.ReadMemStats(memSet)
		data, err := jsonI.Marshal(memSet)
		if err != nil {
			ap.logger.Errorf("AppCmd  receive CMD_MEM  Json Marshal error ", err)
			return
		}
		pack.ResultBytes(data)
	}
}

func (ap *AppCmd) LoopCmdInChan(ctx context.Context) {

	defer func() {
		if r := recover(); r != nil {
			ap.logger.Errorf("AppCmd  LoopCmdInChan  go   %v", string(debug.Stack()))
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case cmdPack, ok := <-ap.inChan:
			if !ok {
				return
			} else {
				ap.handlePack(cmdPack)
			}
		}
	}

}

func (ap *AppCmd) handlePack(pack IPack) {
	if handler, ok := ap.cmdHandlers[pack.GetRouter()]; ok && handler != nil {
		handler(pack)

		replyPack := &config.TSession{
			SrcSubRouter: pack.GetDstSubRouter(),
			DstSubRouter: pack.GetSrcSubRouter(),
			Body:         pack.GetResults(),
			St:           config.TSession_CMD,
			Router:       pack.GetRouter(),
			ReplyToken:   pack.GetReplyToken(),
		}
		if ap.links != nil {
			out, err := proto.Marshal(replyPack)
			if err == nil {
				ap.links.SendMsg(replyPack.GetDstSubRouter(), out)
			} else {
				ap.logger.Infof(" AppCmd  pb Marshal  errr     %v  ", err)
			}
		}
	}
}

func (ap *AppCmd) ReceiveCmdPack(pack IPack) {
	if ap.inChan != nil {
		ap.inChan <- pack
	}
}

func (ap *AppCmd) AddCmdHandler(cmd string, handler HandlerFunc) {
	if len(cmd) > 0 && handler != nil && ap.cmdHandlers != nil {
		ap.handleMutex.Lock()
		ap.cmdHandlers[cmd] = handler
		ap.handleMutex.Unlock()
	}
}

func (ap *AppCmd) Done() {
	if ap.inChan != nil {
		close(ap.inChan)
	}
	if ap.inCancal != nil {
		ap.inCancal()
	}
	if ap.isRuning {
		ap.isRuning = false
	}
}
