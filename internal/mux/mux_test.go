package mux
import ("testing")
func TestOpenStream(t *testing.T){m:=NewMux(10);s,err:=m.OpenStream();if err!=nil||s.State!="open"{t.Error("open")};if m.StreamCount()!=1{t.Error("count")}}
func TestCloseStream(t *testing.T){m:=NewMux(10);s,_:=m.OpenStream();m.CloseStream(s.ID);if m.StreamCount()!=0{t.Error("close")}}
func TestMaxStreams(t *testing.T){m:=NewMux(2);m.OpenStream();m.OpenStream();_,err:=m.OpenStream();if err==nil{t.Error("should fail")}}
func TestSend(t *testing.T){m:=NewMux(10);s,_:=m.OpenStream();m.Handle(func(stream*Stream,data []byte)[]byte{return []byte("echo:"+string(data))});result,_:=m.Send(s.ID,[]byte("hello"));if string(result)!="echo:hello"{t.Error("send")}}
