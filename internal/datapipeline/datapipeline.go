// Package datapipeline provides ETL (Extract, Transform, Load) pipelines
// for agent output data. It supports JSON, CSV, and line-based formats with
// streaming processing, filtering, mapping, and aggregation stages.
package datapipeline

import ("bufio";"encoding/csv";"encoding/json";"fmt";"io";"os";"sort";"strings")

type Stage interface{Process(input []Record)([]Record,error);Name() string}
type Record map[string]any
type Pipeline struct{stages []Stage;name string}
func NewPipeline(name string)*Pipeline{return &Pipeline{name:name}}
func(p*Pipeline)AddStage(s Stage)*Pipeline{p.stages=append(p.stages,s);return p}
func(p*Pipeline)Run(input []Record)([]Record,error){
  data:=input
  for _,s:=range p.stages{var err error;data,err=s.Process(data);if err!=nil{return nil,fmt.Errorf("%s: %w",s.Name(),err)}}
  return data,nil
}
func(p*Pipeline)Stages()int{return len(p.stages)}

type FilterStage struct{name string;fn func(Record)bool}
func NewFilterStage(name string,fn func(Record)bool)*FilterStage{return &FilterStage{name:name,fn:fn}}
func(f *FilterStage)Name()string{return f.name}
func(f *FilterStage)Process(in []Record)([]Record,error){
  var out []Record;for _,r:=range in{if f.fn(r){out=append(out,r)}};return out,nil
}

type MapStage struct{name string;fn func(Record)Record}
func NewMapStage(name string,fn func(Record)Record)*MapStage{return &MapStage{name:name,fn:fn}}
func(m *MapStage)Name()string{return m.name}
func(m *MapStage)Process(in []Record)([]Record,error){
  out:=make([]Record,len(in));for i,r:=range in{out[i]=m.fn(r)};return out,nil
}

type AggregateStage struct{name string;groupBy string;aggFn func([]Record)Record}
func NewAggregateStage(name,groupBy string,fn func([]Record)Record)*AggregateStage{return &AggregateStage{name:name,groupBy:groupBy,aggFn:fn}}
func(a *AggregateStage)Name()string{return a.name}
func(a *AggregateStage)Process(in []Record)([]Record,error){
  groups:=map[string][]Record{}
  for _,r:=range in{key:=fmt.Sprint(r[a.groupBy]);groups[key]=append(groups[key],r)}
  var out []Record;for _,grp:=range groups{out=append(out,a.aggFn(grp))}
  sort.Slice(out,func(i,j int)bool{return fmt.Sprint(out[i][a.groupBy])<fmt.Sprint(out[j][a.groupBy])})
  return out,nil
}

type SortStage struct{name,key string;desc bool}
func NewSortStage(name,key string,desc bool)*SortStage{return &SortStage{name:name,key:key,desc:desc}}
func(s *SortStage)Name()string{return s.name}
func(s *SortStage)Process(in []Record)([]Record,error){
  sort.Slice(in,func(i,j int)bool{
    a,b:=fmt.Sprint(in[i][s.key]),fmt.Sprint(in[j][s.key])
    if s.desc{return a>b};return a<b
  });return in,nil
}

type LimitStage struct{name string;n int}
func NewLimitStage(name string,n int)*LimitStage{return &LimitStage{name:name,n:n}}
func(l *LimitStage)Name()string{return l.name}
func(l *LimitStage)Process(in []Record)([]Record,error){
  if l.n>len(in){l.n=len(in)};return in[:l.n],nil
}

type CountStage struct{name string}
func NewCountStage(name string)*CountStage{return &CountStage{name:name}}
func(c *CountStage)Name()string{return c.name}
func(c *CountStage)Process(in []Record)([]Record,error){return []Record{{"count":len(in)}},nil}

type JSONSource struct{path string}
func NewJSONSource(path string)*JSONSource{return &JSONSource{path:path}}
func(j *JSONSource)Read()([]Record,error){
  f,err:=os.Open(j.path);if err!=nil{return nil,err};defer f.Close()
  var records []Record
  dec:=json.NewDecoder(f);if err:=dec.Decode(&records);err!=nil{
    f.Seek(0,0);scanner:=bufio.NewScanner(f)
    for scanner.Scan(){line:=strings.TrimSpace(scanner.Text());if line==""{continue}
      var rec Record;if err:=json.Unmarshal([]byte(line),&rec);err==nil{records=append(records,rec)}
    }
  }
  return records,nil
}

type CSVSource struct{path string}
func NewCSVSource(path string)*CSVSource{return &CSVSource{path:path}}
func(c *CSVSource)Read()([]Record,error){
  f,err:=os.Open(c.path);if err!=nil{return nil,err};defer f.Close()
  reader:=csv.NewReader(f);headers,err:=reader.Read();if err!=nil{return nil,err}
  var records []Record
  for{row,err:=reader.Read();if err==io.EOF{break}else if err!=nil{return nil,err}
    rec:=Record{};for i,h:=range headers{if i<len(row){rec[h]=row[i]}};records=append(records,rec)
  }
  return records,nil
}

type JSONSink struct{path string}
func NewJSONSink(path string)*JSONSink{return &JSONSink{path:path}}
func(j *JSONSink)Write(records []Record)error{
  f,err:=os.Create(j.path);if err!=nil{return err};defer f.Close()
  enc:=json.NewEncoder(f);enc.SetIndent("","  ");return enc.Encode(records)
}

type CSVSink struct{path string}
func NewCSVSink(path string)*CSVSink{return &CSVSink{path:path}}
func(c *CSVSink)Write(records []Record)error{
  f,err:=os.Create(c.path);if err!=nil{return err};defer f.Close()
  writer:=csv.NewWriter(f);defer writer.Flush()
  if len(records)==0{return nil}
  var headers []string;for k:=range records[0]{headers=append(headers,k)};sort.Strings(headers)
  writer.Write(headers)
  for _,r:=range records{row:=make([]string,len(headers));for i,h:=range headers{row[i]=fmt.Sprint(r[h])};writer.Write(row)}
  return nil
}

func FormatRecords(rs []Record)string{
  if len(rs)==0{return "No records.\n"}
  var sb strings.Builder
  fmt.Fprintf(&sb,"%d record(s):\n",len(rs))
  for i,r:=range rs{if i>=20{fmt.Fprintf(&sb,"  ... and %d more\n",len(rs)-20);break}
    keys:=make([]string,0,len(r));for k:=range r{keys=append(keys,k)};sort.Strings(keys)
    sb.WriteString("  {")
    for j,k:=range keys{if j>0{sb.WriteString(", ")};fmt.Fprintf(&sb,"%s:%v",k,r[k])}
    sb.WriteString("}\n")
  }
  return sb.String()
}
