package authengine
import ("crypto/hmac";"crypto/sha256";"encoding/base64";"encoding/json";"fmt";"net/http";"os";"strings";"sync";"time")
type User struct{ID,Name,Email string;Roles []string;CreatedAt time.Time}
type Token struct{UserID string;ExpiresAt time.Time;Scopes []string}
type Engine struct{mu sync.Mutex;users map[string]*User;tokens map[string]*Token;hmacKey []byte}
func NewEngine()*Engine{key:=[]byte(os.Getenv("LUMEN_AUTH_KEY"));if len(key)==0{key=[]byte("lumen-default-auth-key-change-me")};return &Engine{users:map[string]*User{},tokens:map[string]*Token{},hmacKey:key}}
func(e*Engine)CreateUser(id,name,email string,roles []string)*User{e.mu.Lock();defer e.mu.Unlock();u:=&User{ID:id,Name:name,Email:email,Roles:roles,CreatedAt:time.Now()};e.users[id]=u;return u}
func(e*Engine)GetUser(id string)*User{e.mu.Lock();defer e.mu.Unlock();return e.users[id]}
func(e*Engine)IssueToken(userID string,ttl time.Duration,scopes []string)(string,error){e.mu.Lock();defer e.mu.Unlock();if _,ok:=e.users[userID];!ok{return "",fmt.Errorf("user not found: %s",userID)};tok:=&Token{UserID:userID,ExpiresAt:time.Now().Add(ttl),Scopes:scopes};data,_:=json.Marshal(tok);encoded:=base64.RawURLEncoding.EncodeToString(data);mac:=hmac.New(sha256.New,e.hmacKey);mac.Write([]byte(encoded));sig:=base64.RawURLEncoding.EncodeToString(mac.Sum(nil));tokenStr:=encoded+"."+sig;e.tokens[tokenStr]=tok;return tokenStr,nil}
func(e*Engine)ValidateToken(tokenStr string)(*Token,error){parts:=strings.SplitN(tokenStr,".",2);if len(parts)!=2{return nil,fmt.Errorf("invalid token format")};data,err:=base64.RawURLEncoding.DecodeString(parts[0]);if err!=nil{return nil,err};sig,err:=base64.RawURLEncoding.DecodeString(parts[1]);if err!=nil{return nil,err};mac:=hmac.New(sha256.New,e.hmacKey);mac.Write([]byte(parts[0]));expected:=mac.Sum(nil);if !hmac.Equal(sig,expected){return nil,fmt.Errorf("invalid signature")};var tok Token;json.Unmarshal(data,&tok);if time.Now().After(tok.ExpiresAt){return nil,fmt.Errorf("token expired")};return &tok,nil}
func(e*Engine)RevokeToken(tokenStr string){e.mu.Lock();defer e.mu.Unlock();delete(e.tokens,tokenStr)}
func(e*Engine)HasRole(userID,role string)bool{e.mu.Lock();defer e.mu.Unlock();u,ok:=e.users[userID];if!ok{return false};for _,r:=range u.Roles{if r==role||r=="admin"{return true}};return false}
func(e*Engine)HasScope(token *Token,scope string)bool{for _,s:=range token.Scopes{if s==scope||s=="*"{return true}};return false}
type Middleware struct{engine *Engine}
func NewMiddleware(e *Engine)*Middleware{return &Middleware{engine:e}}
func(m *Middleware)Authenticate(next http.Handler)http.Handler{return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){auth:=r.Header.Get("Authorization");if!strings.HasPrefix(auth,"Bearer "){http.Error(w,"unauthorized",401);return};token,err:=m.engine.ValidateToken(strings.TrimPrefix(auth,"Bearer "));if err!=nil{http.Error(w,"unauthorized: "+err.Error(),401);return};r.Header.Set("X-User-ID",token.UserID);r.Header.Set("X-User-Scopes",strings.Join(token.Scopes,","));next.ServeHTTP(w,r)})}
func(m *Middleware)RequireRole(role string,next http.Handler)http.Handler{return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){userID:=r.Header.Get("X-User-ID");if!m.engine.HasRole(userID,role){http.Error(w,"forbidden",403);return};next.ServeHTTP(w,r)})}
func AdminUser()*User{return &User{ID:"admin",Name:"Admin",Email:"admin@localhost",Roles:[]string{"admin"},CreatedAt:time.Now()}}
func (e *Engine) DefaultAdmin() *User {
  if u := e.GetUser("admin"); u != nil { return u }
  return e.CreateUser("admin", "Admin", "admin@localhost", []string{"admin"})
}
