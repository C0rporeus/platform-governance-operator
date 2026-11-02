import sys, re, json, hashlib, subprocess
from html.parser import HTMLParser
# usage: python3 -I totext.py DIR KIND   (reads DIR/raw.KIND, writes DIR/text.txt)
d, kind = sys.argv[1], sys.argv[2]
SKIP = {"script","style","nav","footer","aside","noscript","svg","form","button","header"}
class P(HTMLParser):
    def __init__(s):
        super().__init__(); s.out=[]; s.skip=0; s.main=0; s.hasmain=False; s.stack=[]
    def handle_starttag(s,t,a):
        a=dict(a)
        if t in ("main","article") : s.main+=1; s.hasmain=True
        if t in SKIP: s.skip+=1
        if t in ("h1","h2","h3","h4","h5","h6"): s.out.append("\n\n"+"#"*int(t[1])+" ")
        elif t in ("p","li","tr","div","pre","blockquote","br"): s.out.append("\n")
        elif t in ("td","th"): s.out.append(" | ")
        elif t=="code": s.out.append("`")
    def handle_endtag(s,t):
        if t in ("main","article"): s.main-=1
        if t in SKIP: s.skip-=1
        if t=="code": s.out.append("`")
        if t in ("p","pre","table","ul","ol"): s.out.append("\n")
    def handle_data(s,x):
        if s.skip<=0: s.out.append(x)
raw=open(f"{d}/raw.{kind}",encoding="utf-8",errors="replace").read() if kind!="pdf" else None
if kind=="pdf":
    subprocess.run(["pdftotext","-layout",f"{d}/raw.pdf",f"{d}/text.txt"],check=True)
elif kind=="md":
    open(f"{d}/text.txt","w").write(raw)
else:
    p=P(); p.feed(raw); txt="".join(p.out)
    txt=re.sub(r"[ \t]+"," ",txt); txt=re.sub(r"\n\s*\n\s*\n+","\n\n",txt)
    open(f"{d}/text.txt","w").write(txt.strip()+"\n")
t=open(f"{d}/text.txt",encoding="utf-8",errors="replace").read()
heads=[l for l in t.splitlines() if re.match(r"^#{1,6} ",l)]
meta={"chars":len(t),"headings":len(heads),"sha256_raw":hashlib.sha256(open(f"{d}/raw.{kind}","rb").read()).hexdigest()}
json.dump(meta,open(f"{d}/meta.json","w"))
print(d.split("/")[-1], meta["chars"], "chars", meta["headings"], "headings")
