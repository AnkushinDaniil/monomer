package node

type SelectiveListener struct {
	OnEngineWebsocketServeErrCb func(error)
	OnCometServeErrCb           func(error)
}

func (s *SelectiveListener) OnEngineWebsocketServeErr(err error) {
	if s.OnEngineWebsocketServeErrCb != nil {
		s.OnEngineWebsocketServeErrCb(err)
	}
}

func (s *SelectiveListener) OnCometServeErr(err error) {
	if s.OnCometServeErrCb != nil {
		s.OnCometServeErrCb(err)
	}
}
