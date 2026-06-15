package blueprint
import ("testing")
func TestResolve(t *testing.T){r:=NewResolver();r.Register(&Component{Name:"a",Factory:func(ctx*Context)(any,error){return"a",nil}});r.Register(&Component{Name:"b",DependsOn:[]string{"a"},Factory:func(ctx*Context)(any,error){return"b",nil}});order,err:=r.Resolve([]string{"b"});if err!=nil{t.Fatal(err)};if len(order)!=2||order[0].Name!="a"{t.Error("order")}}
func TestCircular(t *testing.T){r:=NewResolver();a:=&Component{Name:"a",DependsOn:[]string{"b"},Factory:func(ctx*Context)(any,error){return nil,nil}};b:=&Component{Name:"b",DependsOn:[]string{"a"},Factory:func(ctx*Context)(any,error){return nil,nil}};r.Register(a);r.Register(b);_,err:=r.Resolve([]string{"a"});if err==nil{t.Error("should detect cycle")}}
func TestBuild(t *testing.T){r:=NewResolver();r.RegisterBlueprint(DefaultAgentBlueprint());ctx,cleanup,err:=r.Build([]string{"agent"});if err!=nil{t.Fatal(err)};if ctx==nil{t.Error("ctx")};cleanup()}
func TestValidate(t *testing.T){r:=NewResolver();errs:=r.Validate(&Blueprint{Name:"bad",Components:[]*Component{{Name:"",Factory:func(ctx*Context)(any,error){return nil,nil}}}});if len(errs)==0{t.Error("should catch errors")}}
