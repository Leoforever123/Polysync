package org.polysync.android

import android.app.*
import android.content.*
import android.graphics.Color
import android.graphics.Typeface
import android.net.Uri
import android.os.*
import android.provider.OpenableColumns
import android.view.*
import android.widget.*
import org.json.JSONArray
import org.json.JSONObject
import java.io.File
import java.net.NetworkInterface
import java.util.UUID

class MainActivity : Activity() {
    private val main = Handler(Looper.getMainLooper())
    private lateinit var list: LinearLayout
    private lateinit var scroll: ScrollView
    private lateinit var serviceButton: Button
    private var snapshot = JSONObject()
    private var transfers = JSONArray()
    private var page = "设备"
    private var structure = ""
    private var resumed = false
    private var refreshing = false
    private var dialog: AlertDialog? = null
    private var importPeer = ""
    private var importWorkspace = ""
    private var exportPath = ""
    private val labels = mutableMapOf<String, TextView>()
    private val progress = mutableMapOf<String, ProgressBar>()
    private val navigation = mutableMapOf<String, LinearLayout>()
    private val refresh = object : Runnable {
        override fun run() {
            if (!resumed) return
            if (!refreshing) {
                refreshing = true
                SyncService.worker.execute {
                    try {
                        val engine = SyncService.engine
                        if (engine != null) {
                            val next = JSONObject(engine.request("GET", "/api/status", ""))
                            val tasks = JSONObject(engine.request("GET", "/api/transfers", "")).optJSONArray("tasks") ?: JSONArray()
                            main.post { if (resumed) { snapshot = next; transfers = tasks; display() } }
                        } else main.post { if (resumed) display() }
                    } catch (e: Exception) { main.post { if (resumed) serviceButton.text = "连接中…" } }
                    finally { main.post { refreshing = false } }
                }
            }
            main.postDelayed(this, 2000)
        }
    }
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        if (savedInstanceState != null) {
            importPeer = savedInstanceState.getString("peer", "")
            importWorkspace = savedInstanceState.getString("workspace", "")
            exportPath = savedInstanceState.getString("export", "")
            page = savedInstanceState.getString("page", "设备")
        }
        val outer = column().apply { setBackgroundColor(Design.paper) }
        outer.setOnApplyWindowInsetsListener { view, insets ->
            if (Build.VERSION.SDK_INT >= 30) {
                val bars = insets.getInsets(WindowInsets.Type.systemBars() or WindowInsets.Type.ime())
                view.setPadding(bars.left, bars.top, bars.right, bars.bottom)
            } else view.setPadding(0, insets.systemWindowInsetTop, 0, insets.systemWindowInsetBottom)
            insets
        }
        val header = row().apply { setPadding(22.dp(), 8.dp(), 22.dp(), 4.dp()) }
        header.addView(Mark(this, "brand"), LinearLayout.LayoutParams(32.dp(), 32.dp()))
        header.addView(text("PolySync", 20, bold=true).apply { setPadding(8.dp(),0,0,0) }, LinearLayout.LayoutParams(0, -2, 1f))
        serviceButton = Design.button(this, "启动中") { serviceControls() }.apply { textSize = 12f; tag = "service" }
        header.addView(serviceButton); outer.addView(header)
        scroll = ScrollView(this).apply { isFillViewport = true; clipToPadding = false }
        val centered = android.widget.FrameLayout(this)
        list = column().apply { setPadding(22.dp(), 18.dp(), 22.dp(), 28.dp()) }
        val available = resources.configuration.screenWidthDp
        centered.addView(list, android.widget.FrameLayout.LayoutParams(minOf(available, 620).dp(), -2, Gravity.TOP or Gravity.CENTER_HORIZONTAL))
        scroll.addView(centered); outer.addView(scroll, LinearLayout.LayoutParams(-1, 0, 1f))
        val nav = row().apply { setPadding(16.dp(), 8.dp(), 16.dp(), 8.dp()); background = Design.shape(this@MainActivity, Color.WHITE, 24) }
        for ((key, icon, title) in listOf(Triple("设备", "devices", "设备"), Triple("随传", "send", "随传"), Triple("工作区", "folder", "同步"))) {
            val item = column().apply {
                gravity = Gravity.CENTER; setPadding(8.dp(), 8.dp(), 8.dp(), 8.dp())
                isClickable = true; isFocusable = true; contentDescription = title; tag = "nav:$key"
                importantForAccessibility = View.IMPORTANT_FOR_ACCESSIBILITY_YES
                addView(Mark(this@MainActivity, icon).apply { importantForAccessibility = View.IMPORTANT_FOR_ACCESSIBILITY_NO }, LinearLayout.LayoutParams(24.dp(),24.dp()))
                addView(text(title, 12, bold=true).apply { setPadding(0,5.dp(),0,0); importantForAccessibility = View.IMPORTANT_FOR_ACCESSIBILITY_NO })
                setOnClickListener { page = key; structure = ""; display(); scroll.scrollTo(0,0) }
            }
            navigation[key] = item
            nav.addView(item, LinearLayout.LayoutParams(0, 64.dp(), 1f).apply { marginStart = 4.dp(); marginEnd = 4.dp() })
        }
        outer.addView(nav); setContentView(outer); display()
        if (Build.VERSION.SDK_INT >= 33 && checkSelfPermission(android.Manifest.permission.POST_NOTIFICATIONS) != android.content.pm.PackageManager.PERMISSION_GRANTED) requestPermissions(arrayOf(android.Manifest.permission.POST_NOTIFICATIONS), 90)
        if (!SyncService.userStopped(this)) startSync()
    }
    override fun onSaveInstanceState(outState: Bundle) { outState.putString("peer", importPeer); outState.putString("workspace", importWorkspace); outState.putString("export", exportPath); outState.putString("page",page); super.onSaveInstanceState(outState) }
    override fun onResume() { super.onResume(); resumed = true; SyncService.visible = true; main.post(refresh) }
    override fun onPause() { resumed = false; SyncService.visible = false; main.removeCallbacks(refresh); super.onPause() }
    override fun onDestroy() { dialog?.dismiss(); main.removeCallbacksAndMessages(null); super.onDestroy() }
    private fun startSync() { try { startForegroundService(Intent(this, SyncService::class.java)); serviceButton.text = "启动中…" } catch (e: Exception) { message(e.message ?: "无法启动") } }
    private fun Int.dp() = Design.dp(this@MainActivity, this)
    private fun text(value: String, size: Int = 15, color: Int = Design.ink, bold: Boolean = false) = Design.text(this, value, size, color, bold)
    private fun column() = LinearLayout(this).apply { orientation = LinearLayout.VERTICAL }
    private fun row() = LinearLayout(this).apply { orientation = LinearLayout.HORIZONTAL; gravity = Gravity.CENTER_VERTICAL }
    private fun space(parent: LinearLayout, height: Int = 12) { parent.addView(View(this), LinearLayout.LayoutParams(1,height.dp())) }
    private fun card(color: Int = Color.WHITE): LinearLayout = column().apply {
        background = Design.shape(this@MainActivity, color, 24, if (color == Color.WHITE) Design.line else 0)
        setPadding(20.dp(),20.dp(),20.dp(),20.dp())
        list.addView(this, LinearLayout.LayoutParams(-1,-2).apply { bottomMargin = 12.dp() })
    }
    private fun section(title: String, hint: String = "") {
        space(list,16)
        val head = row(); head.addView(text(title,18,bold=true),LinearLayout.LayoutParams(0,-2,1f))
        if (hint.isNotEmpty()) head.addView(text(hint,11,Design.muted))
        list.addView(head); space(list,12)
    }
    private fun action(parent: LinearLayout, title: String, primary: Boolean = false, id: String = title, click: () -> Unit) {
        parent.addView(Design.button(this,title,primary,click).apply { tag = id }, LinearLayout.LayoutParams(-1,-2).apply { topMargin=10.dp() })
    }
    private fun empty(icon: String, title: String, body: String) {
        val c=card(); c.gravity=Gravity.CENTER
        val circle=android.widget.FrameLayout(this).apply { background=Design.shape(this@MainActivity,Design.mint,24) }
        circle.addView(Mark(this,icon),android.widget.FrameLayout.LayoutParams(30.dp(),30.dp(),Gravity.CENTER))
        c.addView(circle,LinearLayout.LayoutParams(60.dp(),60.dp())); space(c,16)
        c.addView(text(title,17,bold=true).apply { gravity=Gravity.CENTER }); space(c,8)
        c.addView(text(body,13,Design.muted).apply { gravity=Gravity.CENTER })
    }
    private fun serviceControls() {
        val running = SyncService.engine != null
        val current = AlertDialog.Builder(this).setTitle(if (running) "接收服务已开启" else "服务已停止")
            .setMessage("服务开启时，附近设备可以发现这台手机并传输文件。后台空闲 10 分钟后自动停止。\n\n" + SyncService.error)
            .setNegativeButton("返回",null).setPositiveButton(if(running) "停止服务" else "启动服务") { _,_ -> if(running) { SyncService.setUserStopped(this, true); stopService(Intent(this,SyncService::class.java)) } else startSync() }
            .setNeutralButton("重新发现") { _,_ -> startForegroundService(Intent(this,SyncService::class.java).setAction("rediscover")) }.create()
        dialog=current; current.setOnDismissListener { if(dialog===current) dialog=null }; current.show()
    }
    private fun pair(address: String = "", deviceId: String = "") {
        work({ request("POST", "/api/pair/start", JSONObject().put("address",address).put("deviceId",deviceId)) }) { result ->
            val session=JSONObject(result).getString("sessionId")
            prompt("确认是你的设备", "对方允许后，输入六位验证码", 2) { code -> work({ request("POST", "/api/pair/confirm", JSONObject().put("sessionId",session).put("code",code)); "配对成功" }) }
        }
    }
    private fun message(value: String) { if (!isDestroyed) Toast.makeText(this, value, Toast.LENGTH_LONG).show() }
    private fun unpair(peer: JSONObject) {
        val id = peer.getString("id")
        val current = AlertDialog.Builder(this).setTitle("取消配对")
            .setMessage("取消与“${peer.optString("name")}”的配对后，本机不再接受它的随传或同步，关联工作区将停止同步。文件和历史记录保留，仍可在同步页查看与导出。\n\n此操作仅在本机生效，对方需单独取消配对。重新连接前，请在双方取消配对后重新验证，并创建新的同步邀请。")
            .setNegativeButton("保留配对", null)
            .setPositiveButton("取消配对") { _, _ -> work({ request("DELETE", "/api/paired-devices/${Uri.encode(id)}"); "已取消配对，本地文件保留" }) }
            .create()
        dialog = current
        current.setOnDismissListener { if (dialog === current) dialog = null }
        current.show()
        current.getButton(AlertDialog.BUTTON_NEGATIVE).requestFocus()
    }
    private fun work(action: () -> String, success: (String) -> Unit = { message(it) }) {
        SyncService.background.beginWork()
        try {
            SyncService.worker.execute {
                try { val result = action(); main.post { if (!isDestroyed) { success(result); structure = "" } } }
                catch (e: Exception) { main.post { message(e.message ?: "操作失败") } }
                finally { SyncService.background.endWork() }
            }
        } catch (e: Exception) { SyncService.background.endWork(); message(e.message ?: "操作无法启动") }
    }
    private fun engine() = SyncService.engine ?: error("请先启动服务")
    private fun request(method: String, path: String, body: JSONObject = JSONObject()) = engine().request(method, path, body.toString())
    private fun prompt(title: String, hint: String, inputType: Int = 1, accept: (String) -> Unit) {
        val input = EditText(this).apply { this.hint = hint; this.inputType = inputType; setSingleLine() }
        dialog = AlertDialog.Builder(this).setTitle(title).setView(input).setNegativeButton("取消", null)
            .setPositiveButton("确定") { _, _ -> accept(input.text.toString().trim()) }.create()
        val current = dialog
        current?.setOnDismissListener { if (dialog === current) dialog = null }
        dialog?.show(); input.requestFocus()
    }

    private fun addresses(): String = try {
        NetworkInterface.getNetworkInterfaces().toList().flatMap { it.inetAddresses.toList() }
            .filter { !it.isLoopbackAddress && it is java.net.Inet4Address }.joinToString(" / ") { "${it.hostAddress}:45123" }
    } catch (_: Exception) { "连接 Wi-Fi 后显示地址" }
    private fun display() {
        if(isDestroyed) return
        val running = SyncService.engine != null
        serviceButton.text=if(running) "● 接收中" else "○ 已停止"
        serviceButton.contentDescription=if(running) "接收服务已开启，点击管理" else "服务已停止，点击启动"
        for((key,item) in navigation) {
            item.isSelected = page == key
            item.background = android.graphics.drawable.StateListDrawable().apply {
                addState(intArrayOf(android.R.attr.state_focused),Design.shape(this@MainActivity,Design.mint,18,Design.green))
                addState(intArrayOf(),Design.shape(this@MainActivity,if(page==key) Design.mint else Color.TRANSPARENT,18))
            }
        }
        if(dialog!=null) return
        val peers=snapshot.optJSONArray("pairedDevices") ?: JSONArray()
        val nearby=if(running) snapshot.optJSONArray("nearbyDevices") ?: JSONArray() else JSONArray()
        val shares=snapshot.optJSONArray("shares") ?: JSONArray()
        val requests=snapshot.optJSONArray("pairingRequests") ?: JSONArray()
        val invites=snapshot.optJSONArray("shareInvitations") ?: JSONArray()
        val shape=buildString {
            append(page); append(running); append(requests); append(invites)
            val visibleLists=when(page) { "随传" -> listOf(peers,transfers); "工作区" -> listOf(peers,shares); else -> listOf(peers,nearby) }
            for(a in visibleLists) for(i in 0 until a.length()) {
                val o=a.getJSONObject(i); append(o.optString("id"));append(o.optString("name"));append(o.optString("state"));append(o.optBoolean("online")); append(o.optJSONArray("addresses"))
            }
        }
        if(shape!=structure) {
            val focused = currentFocus?.tag
            structure=shape; list.removeAllViews(); labels.clear(); progress.clear()
            when(page) {
                "设备" -> {
                    list.addView(text("局域网直连  /  你的私人连接",11,Design.green,true)); space(list,12)
                    list.addView(text("让设备靠近，\n让文件流动。",30,bold=true)); space(list,12)
                    list.addView(text("连接同一 Wi-Fi，发现身边的电脑和手机。",13,Design.muted)); space(list,24)
                    val self=card(Design.mint)
                    val top=row(); val title=column(); title.addView(text("这台手机",11,Design.muted));space(title,6);title.addView(text(snapshot.optJSONObject("device")?.optString("name") ?: Build.MODEL,19,bold=true))
                    top.addView(title,LinearLayout.LayoutParams(0,-2,1f));top.addView(Mark(this,"phone"),LinearLayout.LayoutParams(32.dp(),32.dp()));self.addView(top);space(self,18)
                    val localAddress=addresses().ifBlank { "连接 Wi-Fi 后显示地址" }
                    self.addView(text(localAddress,12,Design.muted).apply { typeface=Typeface.MONOSPACE; setTextIsSelectable(true) })
                    action(self,"复制连接地址",id="copy-address") { getSystemService(ClipboardManager::class.java).setPrimaryClip(ClipData.newPlainText("PolySync 地址",localAddress)); message("地址已复制") }
                    for(i in 0 until requests.length()) {
                        val r=requests.getJSONObject(i); val id=r.getString("id"); val c=card()
                        c.addView(text("新的配对请求",11,Design.green,true));space(c,8);c.addView(text(r.optString("deviceName"),18,bold=true))
                        if(r.optString("code").isEmpty()) action(c,"允许配对",true,"approve:$id") { work({request("POST","/api/pair/requests/$id/approve");"请在另一台设备输入验证码"}) }
                        else { space(c,14);c.addView(text(r.getString("code"),30,Design.green,true));space(c,8);c.addView(text("请在另一台设备输入此验证码",12,Design.muted)) }
                        action(c,"拒绝",id="reject:$id") { work({request("DELETE","/api/pair/requests/$id");"已拒绝"}) }
                    }
                    section("附近设备","自动发现")
                    val discoveryLabel=text(if(running) SyncService.discoveryStatus else "启动服务后自动发现",12,Design.muted); labels["discovery"]=discoveryLabel; list.addView(discoveryLabel);space(list,12)
                    val found=(0 until nearby.length()).map{nearby.getJSONObject(it)}.filter{it.optBoolean("online") && !it.optBoolean("paired")}
                    if(found.isEmpty()) empty("radar","等待附近的设备","在另一台设备打开 PolySync，\n它就会出现在这里。")
                    for(d in found) {
                        val c=card();c.addView(text(d.optString("name"),18,bold=true));space(c,6);c.addView(text("局域网可连接",12,Design.green))
                        action(c,"连接设备",true,"pair:${d.getString("id")}") { pair(deviceId=d.getString("id")) }
                    }
                    action(list,"找不到设备？手动添加",id="manual-pair") { prompt("手动添加设备","设备地址，例如 192.168.3.5:45123") { pair(address=it) } }
                    if(peers.length()>0) {
                        section("我的设备","${peers.length()} 台已配对")
                        for(i in 0 until peers.length()) {
                            val p=peers.getJSONObject(i);val c=card();c.addView(text(p.optString("name"),18,bold=true));space(c,6)
                            c.addView(text(if(running && p.optBoolean("online")) "● 在线 · 已安全配对" else "已安全配对 · 暂未发现",12,Design.muted))
                            action(c,"发送文件",true,"send:${p.getString("id")}") { chooseImport(p.getString("id"),"") }
                            action(c,"取消配对",id="unpair:${p.getString("id")}") { unpair(p) }
                        }
                    }
                }
                "随传" -> {
                    list.addView(text("随手一传",30,bold=true));space(list,12);list.addView(text("照片、文档、大文件，直接送到你的设备。",13,Design.muted));space(list,24)
                    val send=card(Design.mint);send.addView(Mark(this,"send"),LinearLayout.LayoutParams(38.dp(),38.dp()));space(send,14)
                    send.addView(text("想分享什么？",22,bold=true));space(send,8);send.addView(text("选择一个文件，剩下的交给 PolySync。",13,Design.muted))
                    action(send,"选择文件发送",true) { selectPeer(peers) { chooseImport(it,"") } }
                    section("最近传输","${transfers.length()} 条记录")
                    if(transfers.length()==0) empty("send","第一份文件，从这里出发","接收的文件会保存在应用内，\n你可以随时导出到系统文件夹。")
                    for(i in 0 until transfers.length()) {
                        val t=transfers.getJSONObject(i);val id=t.getString("id");val c=card();val files=t.optJSONArray("files") ?: JSONArray()
                        c.addView(text(files.optJSONObject(0)?.optString("name") ?: "文件传输",17,bold=true));space(c,8)
                        c.addView(text("${if(t.optString("direction")=="send") "发送至" else "来自"} ${t.optString("peerName")}",12,Design.muted));space(c,12)
                        val label=text("",12,Design.muted);labels[id]=label;c.addView(label)
                        val bar=ProgressBar(this,null,android.R.attr.progressBarStyleHorizontal).apply { max=1000; progressTintList=android.content.res.ColorStateList.valueOf(Design.green) };progress[id]=bar;c.addView(bar,LinearLayout.LayoutParams(-1,6.dp()).apply { topMargin=10.dp() })
                        if(t.optString("state") in listOf("preparing","connecting","sending","receiving","verifying")) action(c,"取消传输",id="cancel:$id") { work({request("POST","/api/transfers/$id/cancel");"已取消"}) }
                        if(t.optString("direction")=="send" && t.optString("state") in listOf("failed","cancelled")) action(c,"重试",id="retry:$id") { work({request("POST","/api/transfers/$id/retry");"已重试"}) }
                        if(t.optString("direction")=="receive" && t.optString("state")=="complete") for(j in 0 until files.length()) {
                            val name=files.getJSONObject(j).getString("name");action(c,"保存到手机 · $name",id="export:$id:$j") { work({engine().exportPath(id,j.toLong())}) { prepareExport(it,name) } }
                        }
                    }
                }
                "工作区" -> {
                    list.addView(text("每一份，都同步。",28,bold=true));space(list,12);list.addView(text("在手机和电脑之间，留一份共同的文件。",13,Design.muted));space(list,24)
                    val intro=card(Design.mint);intro.addView(Mark(this,"folder"),LinearLayout.LayoutParams(36.dp(),36.dp()));space(intro,14);intro.addView(text("你的同步工作区",20,bold=true));space(intro,8)
                    intro.addView(text("导入文件副本，与配对设备双向同步。",13,Design.muted))
                    action(intro,"创建工作区",true) { selectPeer(peers) { peer -> prompt("工作区名称","例如：我的文档") { name -> work({engine().createWorkspace(name,peer,"");"邀请已发送，请在对方设备接受"}) } } }
                    for(i in 0 until invites.length()) {val v=invites.getJSONObject(i);val c=card();c.addView(text("收到同步邀请",11,Design.green,true));space(c,8);c.addView(text(v.optString("name"),18,bold=true));action(c,"接受邀请",true,"invite:${v.getString("id")}") {work({engine().createWorkspace(v.optString("name"),"",v.getString("id"));"已创建手机工作区"})}}
                    section("我的工作区","${shares.length()} 个")
                    if(shares.length()==0) empty("folder","给文件一个共同的家","在这里创建工作区，\n或接受电脑发来的文件夹邀请。")
                    for(i in 0 until shares.length()) {
                        val s=shares.getJSONObject(i);val id=s.getString("id");val c=card();c.addView(text(s.optString("name"),18,bold=true));space(c,8);val label=text("",12,Design.muted);labels[id]=label;c.addView(label)
                        if(s.optString("state")=="active") action(c,"立即同步",true,"sync:$id") {work({request("POST","/api/shares/$id/sync");"已开始同步"})}
                        action(c,"导入文件副本",id="import:$id") {chooseImport("",id)}
                        action(c,"查看与导出",id="files:$id") {work({engine().workspaceFiles(id)}) {showFiles(id,JSONArray(it))}}
                    }
                    space(list,12);list.addView(text("工作区保存在应用内，不会修改原始系统文件夹。卸载前，请导出需要保留的文件。",12,Design.muted))
                    if((snapshot.optJSONArray("conflicts")?.length() ?: 0)>0) {space(list,12);list.addView(text("有文件存在同步冲突，双方内容已保留。请在电脑端处理。",13,Design.green))}
                }
            }
            if(focused!=null) list.findViewWithTag<View>(focused)?.requestFocus()
        }
        labels["discovery"]?.text=if(running) SyncService.discoveryStatus else "启动服务后自动发现"
        for(i in 0 until transfers.length()) {
            val t=transfers.getJSONObject(i);val id=t.optString("id");val total=t.optLong("total");val done=t.optLong("done")
            labels[id]?.text="${t.optString("message",t.optString("state"))} · ${android.text.format.Formatter.formatShortFileSize(this,done)} / ${android.text.format.Formatter.formatShortFileSize(this,total)}"
            progress[id]?.progress=if(total>0) (done.toDouble()/total*1000).toInt().coerceIn(0,1000) else if(t.optString("state")=="complete") 1000 else 0
        }
        for(i in 0 until shares.length()) {val s=shares.getJSONObject(i);val runtime=s.optJSONObject("status");labels[s.optString("id")]?.text=if(s.optString("state")=="unpaired") "已取消配对 · 可查看与导出本地文件" else runtime?.optString("message")?.ifEmpty { "准备就绪" } ?: if(s.optString("state")=="pending") "等待对方接受邀请" else "准备就绪"}
    }
    private fun selectPeer(peers: JSONArray, selected: (String) -> Unit) {
        if (peers.length() == 0) { message("请先在设备页配对"); return }
        dialog = AlertDialog.Builder(this).setTitle("选择设备").setItems(Array(peers.length()) { peers.getJSONObject(it).optString("name") }) { _, i -> selected(peers.getJSONObject(i).getString("id")) }.create()
        val current = dialog
        current?.setOnDismissListener { if (dialog === current) dialog = null }; current?.show()
    }
    private fun showFiles(id: String, files: JSONArray) {
        if (files.length() == 0) { message("工作区暂无文件"); return }
        dialog = AlertDialog.Builder(this).setTitle("选择文件导出").setItems(Array(files.length()) { files.getJSONObject(it).getString("name") }) { _, i ->
            val name = files.getJSONObject(i).getString("name")
            work({ engine().workspaceExportPath(id, name) }) { prepareExport(it, name.substringAfterLast('/')) }
        }.create()
        val current = dialog
        current?.setOnDismissListener { if (dialog === current) dialog = null }; current?.show()
    }
    private fun chooseImport(peer: String, workspace: String) {
        importPeer = peer; importWorkspace = workspace
        startActivityForResult(Intent(Intent.ACTION_OPEN_DOCUMENT).setType("*/*").addCategory(Intent.CATEGORY_OPENABLE), 10)
    }
    private fun prepareExport(path: String, name: String) {
        exportPath = path
        startActivityForResult(Intent(Intent.ACTION_CREATE_DOCUMENT).setType("application/octet-stream").addCategory(Intent.CATEGORY_OPENABLE).putExtra(Intent.EXTRA_TITLE, name), 11)
    }
    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        super.onActivityResult(requestCode, resultCode, data)
        val uri = data?.data ?: return
        if (resultCode != RESULT_OK) return
        if (requestCode == 10) {
            val peer = importPeer; val workspace = importWorkspace
            work({
                val name = FileNames.safe(displayName(uri))
                val dir = File(filesDir, "polysync/imports").apply { mkdirs() }
                val imported = File(dir, UUID.randomUUID().toString())
                try {
                    contentResolver.openInputStream(uri).use { input ->
                        requireNotNull(input) { "无法读取文件" }
                        imported.outputStream().use { output ->
                            val buffer = ByteArray(64 * 1024); var total = 0L
                            while (true) { val n = input.read(buffer); if (n < 0) break; total += n; require(total <= 16L * 1024 * 1024 * 1024) { "文件超过 16 GiB" }; require(dir.usableSpace > n + 1024 * 1024) { "手机空间不足" }; output.write(buffer, 0, n) }
                        }
                    }
                    if (workspace.isNotEmpty()) { engine().importToWorkspace(workspace, imported.name, name); "已导入副本，点击立即同步" }
                    else { engine().sendImport(peer, imported.name, name); "已交给后台发送" }
                } finally { imported.delete() }
            })
        } else if (requestCode == 11) {
            val source = exportPath
            work({
                val file = File(source).canonicalFile
                require(file.path.startsWith(File(filesDir, "polysync").canonicalPath + File.separator)) { "导出路径无效" }
                file.inputStream().use { input -> contentResolver.openOutputStream(uri, "wt").use { output -> requireNotNull(output); input.copyTo(output, 64 * 1024) } }
                "文件已导出"
            })
        }
    }
    private fun displayName(uri: Uri): String {
        contentResolver.query(uri, arrayOf(OpenableColumns.DISPLAY_NAME), null, null, null)?.use { if (it.moveToFirst()) return it.getString(0) }
        return "文件"
    }
}
