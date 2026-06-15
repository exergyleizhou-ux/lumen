package archive
import ("testing")
func TestTarGzRoundtrip(t *testing.T){a:=NewArchiver();entries:=[]Entry{{Name:"test.txt",Data:[]byte("hello")}};data,err:=a.CreateTarGz(entries);if err!=nil{t.Fatal(err)};extracted,_:=a.ExtractTarGz(data,"");if len(extracted)!=1||string(extracted[0].Data)!="hello"{t.Error("roundtrip")}}
func TestZipRoundtrip(t *testing.T){a:=NewArchiver();entries:=[]Entry{{Name:"file.txt",Data:[]byte("world")}};data,_:=a.CreateZip(entries);if len(data)<10{t.Error("zip data")}}
func TestSnapshot(t *testing.T){s:=NewSnapshot("/tmp");s.AddFile("a.txt",[]byte("x"));s.AddDir("sub");if s.Count()!=2{t.Error("snapshot count")}}
