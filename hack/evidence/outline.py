import sys, re
t=open(sys.argv[1],encoding="utf-8",errors="replace").read()
cue=re.compile(r"\b(should|must|shall|avoid|ensure|recommend\w*|do not|don't|never|prefer|required|requires?|ought|best practice)\b",re.I)
secs=[]; cur=["(preamble)",0,[]]
for l in t.splitlines():
    m=re.match(r"^(#{1,6}) (.*)",l)
    if m and m.group(2).strip():
        secs.append(cur); cur=[m.group(2).strip()[:70],len(m.group(1)),[]]
    else: cur[2].append(l)
secs.append(cur)
for title,lvl,lines in secs:
    body="\n".join(lines); n=len(body.strip())
    if n==0 and title=="(preamble)": continue
    print(f"{'  '*(lvl-1 if lvl else 0)}{title} [{n}c, {len(cue.findall(body))} cues]")
