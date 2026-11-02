import sys, re, glob
# usage: python3 -I checkquotes.py DOC SOURCES_DIR
doc, src = sys.argv[1], sys.argv[2]
def norm(t):
    t=t.replace("’","'").replace("‘","'").replace("“",'"').replace("”",'"').replace("`","")
    return re.sub(r"\s+"," ",t).lower()
texts={}
for f in glob.glob(f"{src}/*/text.txt"):
    sid=f.split("/")[-2]; texts.setdefault(sid.split("-")[0]+"-"+re.match(r"S-(\d+)",sid).group(1),[]).append(norm(open(f,encoding="utf-8",errors="replace").read()))
body=open(doc,encoding="utf-8").read()
start=body.index("## 4. Practice catalogue"); end=body.index("## 5. Design decisions")
bad=0; total=0
for line in body[start:end].splitlines():
    if not line.startswith("| BP-"): continue
    ids=set(re.findall(r"S-(\d+)",line))
    for q in re.findall(r'"([^"]{8,})"',line):
        total+=1
        qn=norm(q).rstrip(".,;")
        found=any(qn in t for i in ids for t in texts.get("S-"+i,[]))
        if not found:
            bad+=1; print("NOT FOUND:",line.split("|")[1].strip(),"|",q,"| sources",sorted(ids))
print(f"{total} quotes checked, {bad} not found verbatim")
