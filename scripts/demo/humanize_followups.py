# -*- coding: utf-8 -*-
import json, os, psycopg2

TASKS = {
"王雨桐": {
 "title":"给王雨桐发术前注意，确认23号到院",
 "action":"先发一条微信，把角膜剩余厚度、干眼大概多久恢复、术前怎么点药说清楚；再问她23号上午能不能到。",
 "message":"雨桐，我是魏惠铭。今天又把你的角膜数据对了一遍，右眼剩523、左眼520，都在安全范围，不用一直担心。术后干眼一般1-3个月会慢慢缓，前两周会明显一点，我把点药和注意事项发你。23号上午9点前能到吗？"},
"张子涵": {
 "title":"问张子涵妈妈更在意效果还是费用",
 "action":"别一上来推6个月，先问她最担心哪块；把3个月和6个月各几次、多少钱、什么时候复查算清楚发她。",
 "message":"子涵妈妈，我是林晓。上次您说想比较3个月和6个月，我按子涵的情况算了一下：3个月24次3600，6个月48次5780，中间都会复评身高。您更方便先看哪个？不着急定。"},
"赵一诺": {
 "title":"给赵一诺妈妈讲清激素用量，约周末",
 "action":"先把过敏原报告补上，再用大白话讲清楚喷鼻激素的用量和怎么减，别用“依赖”吓她；问她周六上午还是下午方便。",
 "message":"一诺妈妈，我是魏惠铭。过敏原报告我补好了，尘螨4级比较明确。喷鼻激素不是一用就停不下来，我们会按症状减量，我给您写清楚每天几次、用多久。周六上午还是下午方便带孩子来？"},
"周敏": {
 "title":"问周敏产假什么时候结束，先约体验",
 "action":"先不劝她买10次，问她产假结束时间和能不能抽出一次体验；能来再排课。",
 "message":"周敏，我是林晓。上次您说一个人带娃出门难，我先不跟您排10次。您产假大概什么时候结束？结束前要是能抽出一次，我先给您约个体验，看看做着舒不舒服。"},
"刘洋": {
 "title":"记下刘洋的流失原因，一个月后再问候",
 "action":"今天不再追单，记下他选别家是因为价格和当天手术；设一个月后的关心提醒。",
 "message":"刘洋，我是魏惠铭。今天不劝你，已经记下你的选择了。做完如果恢复上有什么想问的，随时找我，不用客气。"},
"孙悦": {
 "title":"给孙悦发同年龄案例，约周宁面诊",
 "action":"先发2个和孙悦年龄相仿、基础相似的案例，标清楚恢复期；再问她哪天方便来让周宁看层次。",
 "message":"孙悦，我是林晓。您担心做完不自然，我找了两个和您年龄、基础差不多的案例，恢复期也标出来了，您先看。方便的话让周宁医生当面看一下层次，再决定要不要做。"},
"李静": {
 "title":"先补李静病历过敏史，再确认周六治疗",
 "action":"先把病历过敏史补上重新提交，别让她白跑；然后问她周六上午还是下午方便。",
 "message":"李静，我是魏惠铭。您的病历过敏史我补上了，重新提交了，不影响周六治疗。周六上午还是下午方便？我提前给您留好时间。"},
"郑好": {
 "title":"跟郑好家长讲6个月怎么复评，安排接送",
 "action":"先回答打针疼不疼、老人接送能不能少跑；把3个月复评节点说清楚，再约陈静面诊。",
 "message":"郑好妈妈，我是林晓。您担心打针疼和老人接送，我先说清楚：6个月不是每个月都来很多次，3个月会复评一次，接送可以按复评节点排。您看哪天方便，让陈静医生再当面讲一下。"},
"许静": {
 "title":"给许静算清3次能看到什么，约夜诊",
 "action":"别只说10次，先告诉她3次看痛经和周期哪几个点；问她周三晚上能不能来。",
 "message":"许静，我是魏惠铭。您担心中药慢，我们先做3次看痛经和周期有没有变化，不用一次买10次。周三晚上刘芳医生在，您几点下班？我给您留个时间。"},
"高远": {
 "title":"问高远出差什么时候回，先约单次体验",
 "action":"他在出差，不卖5次；问回程时间，先约一次单次体验，把恢复期说清楚。",
 "message":"高远，我是林晓。您下个月出差三周，先不做5次，等您回程再约一次单次体验，恢复期我提前跟您说清楚。大概几号回？"},
"唐糖": {
 "title":"术前一天给唐糖讲晶体和运动限制",
 "action":"术前一天别只发流程，重点讲晶体位置、复查时间和多久不能剧烈运动；确认手术排期。",
 "message":"唐糖，我是魏惠铭。手术前一天跟您再对一遍：晶体放好位置后会复查，前两周别做剧烈运动，晚上开车先缓一缓。您的手术日我这边再跟周医生确认，确认后发您。"},
}

def main():
    c=psycopg2.connect(os.environ["DATABASE_URL"]);cur=c.cursor()
    cur.execute("select current_database()")
    if cur.fetchone()[0]!="lingce_dev": raise SystemExit("not dev")
    cur.execute("select recording_id,customer_name,result from deal_expert_analyses where tenant_id=1 and status='analyzed'")
    for rid,name,card in cur.fetchall():
        t=TASKS.get(name)
        if not t: continue
        if isinstance(card,str): card=json.loads(card)
        card['brief']['next_step']=t['title']
        card['primary_action']['goal']=t['title']
        card['primary_action']['action']=t['action']
        card['primary_action']['suggested_content']=t['message']
        cur.execute("update deal_expert_analyses set result=%s where recording_id=%s",(json.dumps(card,ensure_ascii=False),rid))
        cur.execute("update deal_follow_ups set objective=%s,action=%s,suggested_content=%s where tenant_id=1 and recording_id=%s",(t['title'],t['action'],t['message'],rid))
    c.commit();print('humanized',len(TASKS));cur.close();c.close()

if __name__=="__main__": main()
