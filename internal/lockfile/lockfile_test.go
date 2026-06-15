package lockfile
import ("testing";"time")
func TestAcquireRelease(t *testing.T){m:=NewManager();_,err:=m.Acquire("key1","owner1",time.Second);if err!=nil{t.Fatal(err)};err=m.Release("key1","owner1");if err!=nil{t.Error("release")}}
func TestAcquireConflict(t *testing.T){m:=NewManager();m.Acquire("key2","a",time.Hour);_,err:=m.Acquire("key2","b",time.Hour);if err==nil{t.Error("should conflict")}}
func TestRefresh(t *testing.T){m:=NewManager();m.Acquire("key3","o",time.Millisecond*100);err:=m.Refresh("key3","o",time.Hour);if err!=nil{t.Error("refresh")}}
func TestGarbageCollect(t *testing.T){m:=NewManager();m.Acquire("key4","o",0);time.Sleep(time.Millisecond);collected:=m.GarbageCollect();if collected!=1{t.Error("gc")}}
