package org.polysync.android

import android.app.*
import android.content.*
import android.graphics.Color
import android.net.Uri
import android.os.*
import android.provider.OpenableColumns
import android.view.*
import android.widget.*
import org.json.JSONArray
import org.json.JSONObject
import java.io.File
import java.util.UUID

class MainActivity : Activity() {
    private val design by lazy { Design(this) }
    private val main = Handler(Looper.getMainLooper())
    private lateinit var list: LinearLayout
    private lateinit var scroll: ScrollView
    private lateinit var serviceButton: Button
    private var snapshot = JSONObject()
    private var transfers = JSONArray()
    private val pages = listOf("设备", "随传", "工作区")
    private val scrollPositions = mutableMapOf<String, Int>()
    private var historyLimit = 20
    private val transferRows = mutableMapOf<String, LinearLayout>()
    private val transferStates = mutableMapOf<String, List<String>>()
    private val activeTransfers = setOf("preparing","connecting","sending","receiving","verifying")
    private var history: LinearLayout? = null
    private var moreHistory: Button? = null
    private var page = "设备"
    private var structure = ""
    private var resumed = false
    private var refreshing = false
    private var dialog: AlertDialog? = null
    private var afterPair: String? = null
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
                val needTransfers = page == "随传"
                SyncService.worker.execute {
                    try {
                        val engine = SyncService.engine
                        if (engine != null) {
                            val next = JSONObject(engine.request("GET", "/api/status", ""))
                            val tasks = if (needTransfers) JSONObject(engine.request("GET", "/api/transfers", "")).optJSONArray("tasks") ?: JSONArray() else null
                            main.post { if (resumed) { snapshot = next; if (tasks != null) transfers = tasks; display() } }
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
            afterPair = savedInstanceState.getString("afterPair")
            importPeer = savedInstanceState.getString("peer", "")
            importWorkspace = savedInstanceState.getString("workspace", "")
            exportPath = savedInstanceState.getString("export", "")
            page = savedInstanceState.getString("page", "设备")
        }
        val outer = column().apply { setBackgroundColor(design.paper) }
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
        serviceButton = design.button(this, "启动中") { serviceControls() }.apply { textSize = 12f; tag = "service" }
        header.addView(serviceButton); outer.addView(header)
        scroll = ScrollView(this).apply { isFillViewport = true; clipToPadding = false }
        val centered = android.widget.FrameLayout(this)
        list = column().apply { setPadding(22.dp(), 18.dp(), 22.dp(), 28.dp()) }
        val available = resources.configuration.screenWidthDp
        centered.addView(list, android.widget.FrameLayout.LayoutParams(minOf(available, 620).dp(), -2, Gravity.TOP or Gravity.CENTER_HORIZONTAL))
        scroll.addView(centered); outer.addView(scroll, LinearLayout.LayoutParams(-1, 0, 1f))
        val compactNav = resources.configuration.screenHeightDp < 440 && available > resources.configuration.screenHeightDp
        val nav = row().apply { val vertical = if (compactNav) 2.dp() else 8.dp(); setPadding(16.dp(), vertical, 16.dp(), vertical); background = design.shape(this@MainActivity, design.surface, 24) }
        for ((key, icon, title) in listOf(Triple("设备", "devices", "设备"), Triple("随传", "send", "随传"), Triple("工作区", "folder", "同步"))) {
            val item = column().apply {
                if (compactNav) orientation = LinearLayout.HORIZONTAL
                gravity = Gravity.CENTER; minimumHeight = if (compactNav) 48.dp() else 64.dp(); setPadding(8.dp(), 8.dp(), 8.dp(), 8.dp())
                isClickable = true; isFocusable = true; contentDescription = title; tag = "nav:$key"
                importantForAccessibility = View.IMPORTANT_FOR_ACCESSIBILITY_YES
                addView(Mark(this@MainActivity, icon).apply { importantForAccessibility = View.IMPORTANT_FOR_ACCESSIBILITY_NO }, LinearLayout.LayoutParams(24.dp(),24.dp()))
                addView(text(title, 12, bold=true).apply {
                    gravity = Gravity.CENTER; setPadding(if (compactNav) 6.dp() else 0,if (compactNav) 0 else 5.dp(),0,0)
                    importantForAccessibility = View.IMPORTANT_FOR_ACCESSIBILITY_NO
                }, LinearLayout.LayoutParams(-2,-2))
                background = android.graphics.drawable.RippleDrawable(
                    android.content.res.ColorStateList.valueOf((design.green and 0x00ffffff) or 0x22000000),
                    android.graphics.drawable.StateListDrawable().apply {
                        addState(intArrayOf(android.R.attr.state_focused), design.shape(this@MainActivity, design.mint, 16, design.green))
                        addState(intArrayOf(android.R.attr.state_selected), design.shape(this@MainActivity, design.mint, 16))
                        addState(intArrayOf(), design.shape(this@MainActivity, Color.TRANSPARENT, 16))
                    }, null)
                setOnClickListener { afterPair = null; switchPage(key) }
            }
            navigation[key] = item
            nav.addView(item, LinearLayout.LayoutParams(0, -2, 1f).apply { marginStart = 4.dp(); marginEnd = 4.dp() })
        }
        outer.addView(nav); setContentView(outer); display()
        if (!SyncService.userStopped(this)) startSync()
    }
    override fun onSaveInstanceState(outState: Bundle) { outState.putString("afterPair",afterPair); outState.putString("peer", importPeer); outState.putString("workspace", importWorkspace); outState.putString("export", exportPath); outState.putString("page",page); super.onSaveInstanceState(outState) }
    override fun onResume() { super.onResume(); resumed = true; SyncService.visible = true; main.post(refresh) }
    override fun onPause() { resumed = false; SyncService.visible = false; main.removeCallbacks(refresh); super.onPause() }
    override fun onDestroy() { dialog?.dismiss(); main.removeCallbacksAndMessages(null); super.onDestroy() }
    private fun switchPage(next: String) {
        if (page == next) return
        val direction = pages.indexOf(next).compareTo(pages.indexOf(page))
        scrollPositions[page] = scroll.scrollY
        page = next
        display()
        scroll.scrollTo(0, scrollPositions[next] ?: 0)
        scroll.animate().cancel()
        if (android.animation.ValueAnimator.areAnimatorsEnabled()) {
            scroll.translationX = direction * 8.dp().toFloat()
            scroll.alpha = .88f
            scroll.animate().translationX(0f).alpha(1f).setDuration(160)
                .setInterpolator(android.view.animation.PathInterpolator(.22f, 1f, .36f, 1f)).start()
        } else {
            scroll.translationX = 0f
            scroll.alpha = 1f
        }
        refreshSoon()
    }
    private fun refreshSoon() {
        if (resumed) {
            main.removeCallbacks(refresh)
            main.post(refresh)
        }
    }
    private fun startSync() { try { startForegroundService(Intent(this, SyncService::class.java)); serviceButton.text = "启动中…" } catch (e: Exception) { message(e.message ?: "无法启动") } }
    private fun Int.dp() = design.dp(this@MainActivity, this)
    private fun text(value: String, size: Int = 15, color: Int = design.ink, bold: Boolean = false) = design.text(this, value, size, color, bold)
    private fun column() = LinearLayout(this).apply { orientation = LinearLayout.VERTICAL }
    private fun row() = LinearLayout(this).apply { orientation = LinearLayout.HORIZONTAL; gravity = Gravity.CENTER_VERTICAL }
    private fun space(parent: LinearLayout, height: Int = 12) { parent.addView(View(this), LinearLayout.LayoutParams(1,height.dp())) }
    private fun card(color: Int = design.surface, parent: LinearLayout = list): LinearLayout = column().apply {
        background = design.shape(this@MainActivity, color, 18, if (color == design.surface) design.line else 0)
        setPadding(20.dp(),20.dp(),20.dp(),20.dp())
        parent.addView(this, LinearLayout.LayoutParams(-1,-2).apply { bottomMargin = 12.dp() })
    }
    private fun section(title: String, hint: String = "") {
        space(list,16)
        val head = row(); head.addView(text(title,18,bold=true),LinearLayout.LayoutParams(0,-2,1f))
        if (hint.isNotEmpty()) head.addView(text(hint,11,design.muted))
        list.addView(head); space(list,12)
    }
    private fun action(parent: LinearLayout, title: String, primary: Boolean = false, id: String = title, click: () -> Unit) {
        parent.addView(design.button(this,title,primary,click).apply { tag = id }, LinearLayout.LayoutParams(-1,-2).apply { topMargin=10.dp() })
    }
    private fun asyncAction(parent: LinearLayout, label: String, pending: String, primary: Boolean, id: String,
                            operation: () -> String, success: (String) -> Unit = ::message) {
        lateinit var button: Button
        button = design.button(this,label,primary) {
            button.isEnabled = false
            button.text = pending
            work(operation, { error ->
                button.text = label; button.isEnabled = true; message(error)
            }) { result ->
                button.text = label; button.isEnabled = true
                if (resumed && button.isAttachedToWindow) success(result)
            }
        }.apply { tag = id }
        parent.addView(button,LinearLayout.LayoutParams(-1,-2).apply { topMargin = 10.dp() })
    }
    private fun empty(icon: String, title: String, body: String) {
        val c=card(); c.gravity=Gravity.CENTER
        val circle=android.widget.FrameLayout(this).apply { background=design.shape(this@MainActivity,design.mint,24) }
        circle.addView(Mark(this,icon),android.widget.FrameLayout.LayoutParams(30.dp(),30.dp(),Gravity.CENTER))
        c.addView(circle,LinearLayout.LayoutParams(60.dp(),60.dp())); space(c,16)
        c.addView(text(title,17,bold=true).apply { gravity=Gravity.CENTER }); space(c,8)
        c.addView(text(body,13,design.muted).apply { gravity=Gravity.CENTER })
    }
    private fun serviceControls() {
        val running = SyncService.engine != null
        val builder = AlertDialog.Builder(this).setTitle(if (running) "接收服务已开启" else "接收服务已停止")
            .setMessage(SyncService.error.ifBlank { "服务开启时可以接收文件，后台空闲 10 分钟后自动停止。" })
            .setNegativeButton("关闭",null)
            .setPositiveButton(if (running) "停止服务" else "启动服务") { _,_ ->
                if (running) {
                    SyncService.setUserStopped(this, true)
                    stopService(Intent(this, SyncService::class.java))
                } else startSync()
                refreshSoon()
            }
        if (running && Build.VERSION.SDK_INT >= 33 && checkSelfPermission(android.Manifest.permission.POST_NOTIFICATIONS) != android.content.pm.PackageManager.PERMISSION_GRANTED) {
            builder.setNeutralButton("开启后台通知") { _,_ -> requestPermissions(arrayOf(android.Manifest.permission.POST_NOTIFICATIONS), 20) }
        } else if (running) {
            builder.setNeutralButton("重新发现") { _,_ -> startForegroundService(Intent(this,SyncService::class.java).setAction("rediscover")); message("正在重新查找设备") }
        }
        showDialog(builder.create())
    }

    private fun pairStart(address: String, deviceId: String) =
        request("POST", "/api/pair/start", JSONObject().put("address",address).put("deviceId",deviceId))

    private fun confirmPair(result: String) {
        val session = JSONObject(result).getString("sessionId")
        inputRequest("确认是你的设备", "对方允许后，输入六位验证码", "完成配对", android.text.InputType.TYPE_CLASS_NUMBER,
            validate = { if (!it.matches(Regex("[0-9]{6}"))) "请输入六位数字" else null },
            action = { code -> request("POST","/api/pair/confirm", JSONObject().put("sessionId",session).put("code",code)) }) {
            message("配对成功")
            val next = afterPair
            afterPair = null
            if (next != null) switchPage(next)
        }
    }

    private fun message(value: String) { if (!isDestroyed) Toast.makeText(this, value, Toast.LENGTH_LONG).show() }
    private fun unpair(peer: JSONObject) {
        val id = peer.getString("id")
        val body = "此手机将停止与“${peer.optString("name")}”收发和同步。已保存的文件可在同步页查看与导出。\n\n对方需单独取消配对。重新连接时，双方需重新配对并发送同步邀请。"
        val current = AlertDialog.Builder(this).setTitle("取消与${peer.optString("name")}的配对？")
            .setMessage(body).setNegativeButton("保留配对", null).setPositiveButton("取消配对", null).create()
        showDialog(current)
        current.getButton(AlertDialog.BUTTON_NEGATIVE).requestFocus()
        current.getButton(AlertDialog.BUTTON_POSITIVE).setOnClickListener {
            submitDialog(current,"正在取消…", { request("DELETE","/api/paired-devices/${Uri.encode(id)}") },
                { error -> current.setMessage("$body\n\n$error") }) { message("已取消配对，本地文件保留") }
        }
    }

    private fun work(action: () -> String, failure: (String) -> Unit = ::message, success: (String) -> Unit = ::message) {
        SyncService.background.beginWork()
        try {
            SyncService.worker.execute {
                try { val result = action(); main.post { if (!isDestroyed) { success(result); refreshSoon() } } }
                catch (e: Exception) { main.post { if (!isDestroyed) failure(e.message ?: "操作失败，请重试") } }
                finally { SyncService.background.endWork() }
            }
        } catch (e: Exception) { SyncService.background.endWork(); failure(e.message ?: "操作无法启动") }
    }
    private fun engine() = SyncService.engine ?: error("请先启动服务")
    private fun request(method: String, path: String, body: JSONObject = JSONObject()) = engine().request(method, path, body.toString())

    private fun submitDialog(current: AlertDialog, pending: String, action: () -> String, failure: (String) -> Unit = ::message, success: (String) -> Unit) {
        val button = current.getButton(AlertDialog.BUTTON_POSITIVE)
        if (!button.isEnabled) return
        val title = button.text
        button.isEnabled = false
        button.text = pending
        work(action, { reason ->
            if (current.isShowing) { button.isEnabled = true; button.text = title; failure(reason) }
        }) { result ->
            if (current.isShowing) {
                current.dismiss()
                if (resumed) success(result)
            }
        }
    }

    private fun inputRequest(title: String, hint: String, submit: String, type: Int, validate: (String) -> String?,
                             action: (String) -> String, success: (String) -> Unit) {
        val input = EditText(this).apply { this.hint = hint; inputType = type; setSingleLine() }
        val fields = column().apply { setPadding(24.dp(),8.dp(),24.dp(),0); addView(input) }
        val current = AlertDialog.Builder(this).setTitle(title).setView(fields)
            .setNegativeButton("取消",null).setPositiveButton(submit,null).create()
        showDialog(current)
        current.getButton(AlertDialog.BUTTON_POSITIVE).setOnClickListener {
            val value = input.text.toString().trim()
            val problem = validate(value)
            if (problem != null) { input.error = problem; input.requestFocus(); return@setOnClickListener }
            submitDialog(current,"正在连接…", { action(value) }, { error -> input.error = error; input.requestFocus() }, success)
        }
        input.requestFocus()
    }

    private fun addresses(): List<String> {
        val values = snapshot.optJSONObject("device")?.optJSONArray("addresses") ?: return emptyList()
        return (0 until values.length()).map { values.optString(it) }.filter { it.isNotBlank() && !it.startsWith("127.") }
    }
    private fun display() {
        if(isDestroyed) return
        val running = SyncService.engine != null
        serviceButton.text=if(running) "● 接收中" else "○ 已停止"
        serviceButton.contentDescription=if(running) "接收服务已开启，点击管理" else "服务已停止，点击启动"
        for((key,item) in navigation) {
            item.isSelected = page == key
        }
        if(dialog?.isShowing == true) return
        val peers=snapshot.optJSONArray("pairedDevices") ?: JSONArray()
        val nearby=if(running) snapshot.optJSONArray("nearbyDevices") ?: JSONArray() else JSONArray()
        val shares=snapshot.optJSONArray("shares") ?: JSONArray()
        val requests=snapshot.optJSONArray("pairingRequests") ?: JSONArray()
        val invites=snapshot.optJSONArray("shareInvitations") ?: JSONArray()
        val conflicts = if (page == "工作区") snapshot.optJSONArray("conflicts")?.objects().orEmpty().count { it.optString("status") == "pending" } else 0
        val shape=buildString {
            append(page); append(running)
            if (page == "设备") { append(requests); append(snapshot.optJSONObject("device")?.optString("name")) }
            if (page == "工作区") { append(invites); append(conflicts) }
            val visibleLists=when(page) { "随传" -> listOf(peers); "工作区" -> listOf(peers,shares); else -> listOf(peers,nearby) }
            for(a in visibleLists) for(i in 0 until a.length()) {
                val o=a.getJSONObject(i); append(o.optString("id"));append(o.optString("name"));append(o.optString("state"));append(o.optBoolean("online")); append(o.optString("localRole")); append(o.optJSONArray("members"))
            }
        }
        if(shape!=structure) {
            val focused = currentFocus?.tag
            structure=shape; list.removeAllViews(); labels.clear(); progress.clear(); transferRows.clear(); transferStates.clear(); history = null; moreHistory = null
            when(page) {
                "设备" -> {
                    list.addView(text("设备",28,bold=true)); space(list,16)
                    val self=card(design.mint)
                    val top=row(); val title=column(); title.addView(text("这台手机",11,design.muted));space(title,6);title.addView(text(snapshot.optJSONObject("device")?.optString("name") ?: Build.MODEL,19,bold=true))
                    top.addView(title,LinearLayout.LayoutParams(0,-2,1f));top.addView(Mark(this,"phone"),LinearLayout.LayoutParams(32.dp(),32.dp()));self.addView(top);space(self,18)
                    if (running) action(self,"查看连接地址",id="copy-address") {
                        val localAddress = addresses()
                        val addressDialog = AlertDialog.Builder(this).setTitle("本机连接地址")
                            .setMessage(if (localAddress.isEmpty()) "连接 Wi-Fi 后可查看地址。" else localAddress.joinToString("\n"))
                            .setNegativeButton("关闭", null)
                        if (localAddress.isNotEmpty()) addressDialog.setPositiveButton("复制地址") { _, _ ->
                            getSystemService(ClipboardManager::class.java).setPrimaryClip(ClipData.newPlainText("PolySync 地址", localAddress.first()))
                            message("地址已复制")
                        }
                        showDialog(addressDialog.create())
                    }
                    for(i in 0 until requests.length()) {
                        val r=requests.getJSONObject(i); val id=r.getString("id"); val c=card()
                        c.addView(text("新的配对请求",11,design.green,true));space(c,8);c.addView(text(r.optString("deviceName"),18,bold=true))
                        if(r.optString("code").isEmpty()) action(c,"允许配对",true,"approve:$id") { work({request("POST","/api/pair/requests/$id/approve");"请在另一台设备输入验证码"}) }
                        else { space(c,14);c.addView(text(r.getString("code"),30,design.green,true));space(c,8);c.addView(text("请在另一台设备输入此验证码",12,design.muted)) }
                        action(c,"拒绝",id="reject:$id") { work({request("DELETE","/api/pair/requests/$id");"已拒绝"}) }
                    }
                    section("附近设备")
                    val discoveryLabel=text(if(running) SyncService.discoveryStatus else "启动服务后自动发现",12,design.muted); labels["discovery"]=discoveryLabel; list.addView(discoveryLabel)
                    val found=(0 until nearby.length()).map{nearby.getJSONObject(it)}.filter{it.optBoolean("online") && !it.optBoolean("paired")}
                    if (found.isEmpty() && running && peers.length() == 0) empty("radar","在另一台设备上打开 PolySync","连接同一 Wi-Fi 后，附近设备会出现在这里。")
                    for(d in found) {
                        val c=card();c.addView(text(d.optString("name"),18,bold=true));space(c,6);c.addView(text("局域网可连接",12,design.green))
                        asyncAction(c,"连接设备","正在连接…",true,"pair:${d.getString("id")}", { pairStart("",d.getString("id")) }) { if (dialog == null) confirmPair(it) }
                    }
                    if (running) action(list,"输入设备地址",id="manual-pair") {
                        inputRequest("输入设备地址", "例如 192.168.1.10:45123", "连接设备", android.text.InputType.TYPE_CLASS_TEXT or android.text.InputType.TYPE_TEXT_VARIATION_URI,
                            validate={ if (it.isEmpty()) "请输入对方的连接地址" else null }, action={ pairStart(it,"") }, success=::confirmPair)
                    }
                    if(peers.length()>0) {
                        section("我的设备","${peers.length()} 台已配对")
                        for(i in 0 until peers.length()) {
                            val p=peers.getJSONObject(i);val c=card();c.addView(text(p.optString("name"),18,bold=true));space(c,6)
                            c.addView(text(if(running && p.optBoolean("online")) "● 在线 · 已安全配对" else "已安全配对 · 暂未发现",12,design.muted))
                            if (running) action(c,"发送文件",true,"send:${p.getString("id")}") { chooseImport(p.getString("id"),"") }
                            action(c,"取消配对",id="unpair:${p.getString("id")}") { unpair(p) }
                        }
                    }
                }
                "随传" -> {
                    list.addView(text("随传",28,bold=true)); space(list,16)
                    val send=card(design.mint)
                    send.addView(text("发送文件",20,bold=true))
                    val options = peers.objects()
                    if (running && options.isNotEmpty()) {
                        val suggested = options.singleOrNull()
                        val title = suggested?.let { "发送给 " + it.optString("name") } ?: "选择接收设备"
                        action(send,title,true,"send-file") { selectPeer(options) { chooseImport(it,"") } }
                    } else {
                        space(send,8)
                        send.addView(text(if (running) "配对后就能直接收发文件。" else "接收服务已停止。",14,design.muted))
                        if (running) action(send,"添加设备",true,"send-pair") { afterPair = page; switchPage("设备") }
                        else action(send,"启动接收服务",true) { startSync() }
                    }
                    section("最近传输")
                    val holder = column()
                    list.addView(holder)
                    history = holder
                    val more = design.button(this,"显示更多") { historyLimit += 20; updateTransfers() }
                    list.addView(more)
                    moreHistory = more
                }
                "工作区" -> {
                    list.addView(text("同步",28,bold=true));space(list,16)
                    val intro=card(design.mint)
                    intro.addView(text("手机工作区",20,bold=true)); space(intro,8)
                    intro.addView(text("将文件副本导入工作区后，即可与其他设备同步。",14,design.muted))
                    if (!running) action(intro,"启动接收服务",true) { startSync() }
                    else if (peers.length() == 0) action(intro,"添加设备",true,"workspace-pair") { afterPair = page; switchPage("设备") }
                    else action(intro,"创建工作区",true) { workspaceInvite(peers.objects(), null) }
                    for (v in invites.objects()) {
                        val c=card()
                        c.addView(text(v.optString("name"),18,bold=true)); space(c,6)
                        c.addView(text("${v.optString("deviceName")} 邀请此手机${roleLabel(v)}",14,design.muted))
                        if (running) asyncAction(c,"接受邀请","正在加入…",true,"invite:${v.getString("id")}", {
                            engine().createWorkspace(v.optString("name"),"",v.getString("id")); "已加入工作区"
                        })
                    }
                    if(shares.length() > 0) section("我的工作区")
                    for(i in 0 until shares.length()) {
                        val s=shares.getJSONObject(i);val id=s.getString("id");val c=card();c.addView(text(s.optString("name"),18,bold=true));space(c,8);val label=text("",12,design.muted);labels[id]=label;c.addView(label)
                        if(running && s.optString("state")=="active") asyncAction(c,"立即同步","正在开始…",true,"sync:$id",{ request("POST","/api/shares/$id/sync"); "已开始同步" })
                        val members = s.optJSONArray("members")?.objects().orEmpty()
                        if (s.optString("state") in setOf("active", "pending", "disconnected") || (s.optString("state") == "unpaired" && s.optString("localRole") == "collaborator")) {
                            val role = s.optString("localRole").ifBlank { "collaborator" }
                            for (member in members.filter { it.optString("state") != "unpaired" }) {
                                val name = peers.objects().firstOrNull { it.optString("id") == member.optString("deviceId") }?.optString("name") ?: "已配对设备"
                                val detail = if (role == "follower") {
                                    if (member.optBoolean("writeBackPending")) "跟随 $name · 回传前需核对本机修改" else if (member.optBoolean("allowWriteBack")) "跟随 $name · 本机修改会回传" else "跟随 $name · 本机只接收副本"
                                } else "$name · ${roleLabel(member)}"
                                c.addView(text(detail + if (member.optString("state") == "pending") " · 等待接受" else "",13,design.muted))
                            }
                            val connected = members.filter { it.optString("state") != "unpaired" }
                            if (running && role == "follower" && connected.any { it.optBoolean("writeBackPending") }) {
                                asyncAction(c,"核对待回传内容","正在检查…",false,"preview:$id",{
                                    request("GET","/api/shares/$id/writeback-preview")
                                }) { previewWriteBack(s,JSONObject(it)) }
                            }
                            if (running && connected.isNotEmpty()) action(c,"成员与权限",id="manage:$id") {
                                manageMembers(s,connected,peers.objects())
                            }
                            val existing = if (s.optString("localRole").isBlank()) setOf(s.optString("peerDeviceId")) else connected.map { it.optString("deviceId") }.toSet()
                            val candidates = peers.objects().filter { it.optString("id") !in existing }
                            if (running && role != "follower" && candidates.isNotEmpty()) action(c,"邀请设备",id="member:$id") { workspaceInvite(candidates, s) }
                            if (running && s.optString("state") == "active" && (role != "follower" || members.any { it.optBoolean("allowWriteBack") && !it.optBoolean("writeBackPending") })) action(c,"导入文件副本",id="import:$id") { chooseImport("",id) }
                        }
                        if (running) action(c,"查看与导出",id="files:$id") {work({engine().workspaceFiles(id)}) {showFiles(id,JSONArray(it))}}
                    }
                    if (shares.length() > 0) list.addView(text("工作区保存在应用内。卸载前，请导出需要保留的文件。",12,design.muted))
                    if (conflicts > 0) {space(list,12);list.addView(text("有文件存在同步冲突，双方内容已保留。请在电脑端处理。",13,design.green))}
                }
            }
            if(focused!=null) list.findViewWithTag<View>(focused)?.requestFocus()
        }
        labels["discovery"]?.let { label ->
            val detail = if (running) SyncService.discoveryStatus else "启动接收服务后可发现设备"
            if (label.text != detail) label.text = detail
            label.visibility = if (detail == "正在发现同一网络的设备") View.GONE else View.VISIBLE
        }
        if (page == "随传") updateTransfers()
        if (page != "工作区") return
        for (s in shares.objects()) {
            val status = when (s.optString("state")) {
                "unpaired" -> "已取消配对 · 可查看与导出本地文件"
                "disconnected" -> "已停止同步 · 本地文件已保留"
                "pending" -> "等待对方接受邀请"
                else -> s.optJSONObject("status")?.optString("message").orEmpty().ifBlank { "准备就绪" }
            }
            val id = s.optString("id")
            labels[id]?.let { if (it.text != status) it.text = status }
            list.findViewWithTag<Button>("sync:$id")?.let { button ->
                if (button.text != "正在开始…") {
                    val busy = s.optJSONObject("status")?.optString("state") == "syncing"
                    val title = if (busy) "同步中…" else "立即同步"
                    if (button.text != title) button.text = title
                    button.isEnabled = !busy
                }
            }
        }
    }
    private fun updateTransfers() {
        val holder = history ?: return
        val count = minOf(transfers.length(), historyLimit)
        val tasks = (0 until count).mapNotNull { transfers.optJSONObject(it) }
        val ids = tasks.map { it.getString("id") }.toSet()
        for (id in transferRows.keys - ids) {
            holder.removeView(transferRows.remove(id))
            transferStates.remove(id); labels.remove(id); progress.remove(id)
        }
        val empty = holder.findViewWithTag<TextView>("empty-history")
        if (tasks.isEmpty() && empty == null) holder.addView(text("暂无传输记录",14,design.muted).apply { tag = "empty-history" })
        if (tasks.isNotEmpty() && empty != null) holder.removeView(empty)
        moreHistory?.visibility = if (count < transfers.length()) View.VISIBLE else View.GONE
        for ((index, task) in tasks.withIndex()) {
            val id = task.getString("id")
            val state = task.optString("state")
            val active = state in activeTransfers
            val files = task.optJSONArray("files")?.objects().orEmpty()
            val sending = task.optString("direction") == "send"
            val names = files.map { it.optString("name") }
            val layout = listOf(if (active) "active" else state, task.optString("direction"), task.optString("peerName")) +
                names + files.map { if (it.optString("state") == "missing") "missing" else "" }
            val item = transferRows.getOrPut(id) { card(parent=holder).apply { tag = "transfer:$id" } }
            if (holder.indexOfChild(item) != index) {
                holder.removeView(item); holder.addView(item,index)
            }
            if (transferStates[id] != layout) {
                val focus = item.findFocus()?.tag
                transferStates[id] = layout
                item.removeAllViews()
                val title = (names.firstOrNull() ?: "文件") + if (files.size > 1) " 等 ${files.size} 个文件" else ""
                item.addView(text(title,17,bold=true)); space(item,6)
                item.addView(text((if (sending) "发送给 " else "来自 ") + task.optString("peerName"),13,design.muted)); space(item,8)
                val status = text("",13,design.muted)
                labels[id] = status; item.addView(status)
                progress.remove(id)
                if (active) {
                    val bar = ProgressBar(this,null,android.R.attr.progressBarStyleHorizontal).apply {
                        max = 1000; progressTintList = android.content.res.ColorStateList.valueOf(design.green)
                    }
                    progress[id] = bar
                    item.addView(bar, LinearLayout.LayoutParams(-1,8.dp()).apply { topMargin = 6.dp() })
                    asyncAction(item,"取消传输","正在取消…",false,"cancel:$id",{ request("POST","/api/transfers/$id/cancel"); "已取消传输" })
                }
                if (sending && state in setOf("failed","cancelled"))
                    asyncAction(item,"重新发送","正在准备…",false,"retry:$id",{ request("POST","/api/transfers/$id/retry"); "正在重新发送" })
                if (!sending && state == "complete") for ((j,file) in files.withIndex()) {
                    if (file.optString("state") == "missing") {
                        item.addView(text("${names[j]} 已移出接收目录",13,design.muted))
                    } else action(item,"保存到手机 · ${names[j]}",id="export:$id:$j") {
                        work({ engine().exportPath(id,j.toLong()) }) { prepareExport(it,names[j]) }
                    }
                }
                if (focus != null) item.findViewWithTag<View>(focus)?.requestFocus()
            }
            val total = task.optLong("total"); val done = task.optLong("done")
            val status = task.optString("message").ifBlank { when (state) {
                "complete" -> "已完成"; "failed" -> "传输失败"; "cancelled" -> "已取消"
                "sending" -> "正在发送"; "receiving" -> "正在接收"; "verifying" -> "正在校验"
                "connecting" -> "正在连接"; else -> "准备中"
            } }
            val size = android.text.format.Formatter.formatShortFileSize(this,total)
            val label = if (active) "$status · ${android.text.format.Formatter.formatShortFileSize(this,done)} / $size" else "$status · $size"
            labels[id]?.let { if (it.text != label) it.text = label }
            progress[id]?.let { bar ->
                val value = if (total > 0) (done.toDouble()/total*bar.max).toInt().coerceIn(0,bar.max) else 0
                if (bar.progress != value) bar.setProgress(value,bar.isLaidOut && android.animation.ValueAnimator.areAnimatorsEnabled())
            }
        }
    }

    private fun JSONArray.objects(): List<JSONObject> = (0 until length()).mapNotNull { optJSONObject(it) }

    private fun showDialog(current: AlertDialog) {
        dialog = current
        current.setOnDismissListener { if (dialog === current) { dialog = null; if (resumed) display() } }
        current.show()
    }

    private fun selectPeer(peers: List<JSONObject>, selected: (String) -> Unit) {
        if (peers.isEmpty()) { switchPage("设备"); return }
        val only = peers.singleOrNull()
        if (only != null) { selected(only.getString("id")); return }
        showDialog(AlertDialog.Builder(this).setTitle("选择设备")
            .setItems(peers.map { it.optString("name") + if (it.optBoolean("online")) "" else " · 离线" }.toTypedArray()) { _, i ->
                selected(peers[i].getString("id"))
            }.setNegativeButton("取消",null).create())
    }

    private fun roleLabel(invite: JSONObject) = when {
        invite.optString("role") != "follower" -> "共同编辑"
        invite.optBoolean("allowWriteBack") -> "跟随此文件夹并回传修改"
        else -> "跟随此文件夹（仅接收）"
    }

    private fun confirmOperation(title: String, detail: String, confirm: String, pending: String,
                                 action: () -> String, success: String) {
        val current = AlertDialog.Builder(this).setTitle(title).setMessage(detail)
            .setNegativeButton("取消",null).setPositiveButton(confirm,null).create()
        showDialog(current)
        current.getButton(AlertDialog.BUTTON_NEGATIVE).requestFocus()
        current.getButton(AlertDialog.BUTTON_POSITIVE).setOnClickListener {
            submitDialog(current,pending,action,{ error -> current.setMessage("$detail\n\n$error") }) { message(success) }
        }
    }

    private fun manageMembers(share: JSONObject, members: List<JSONObject>, peers: List<JSONObject>) {
        fun name(member: JSONObject) = peers.firstOrNull { it.optString("id") == member.optString("deviceId") }?.optString("name") ?: "已配对设备"
        fun manage(member: JSONObject) {
            val device = name(member)
            val endpoint = "/api/shares/${Uri.encode(share.getString("id"))}/members/${Uri.encode(member.getString("deviceId"))}"
            val folder = share.optString("name")
            val options = mutableListOf<String>()
            val writable = member.optBoolean("allowWriteBack")
            val canSetWrite = member.optString("role") == "follower" && member.optString("state") == "active"
            if (canSetWrite) options.add(if (writable) "关闭回传" else "允许回传")
            options.add("停止与此设备同步")
            showDialog(AlertDialog.Builder(this).setTitle(device)
                .setItems(options.toTypedArray()) { _, index ->
                    if (canSetWrite && index == 0) {
                        val detail = if (writable) "此设备仍会接收“$folder”的更新，今后的本地修改将保留在它自己的副本中。"
                            else "对方须先核对本地修改并确认。确认后，它的修改和删除会同步至“$folder”。"
                        confirmOperation(options[index],detail,options[index],"正在保存…",
                            { request("PUT",endpoint,JSONObject().put("allowWriteBack",!writable)) },if (writable) "已关闭回传" else "已允许对方核对回传")
                    } else {
                        confirmOperation("停止与${device}同步？","此手机将停止与它同步“$folder”。双方已保存的文件和设备配对会保留。",
                            "停止同步","正在停止…",{ request("DELETE",endpoint) },"已停止与此设备同步")
                    }
                }.setNegativeButton("关闭",null).create())
        }
        if (members.size == 1) { manage(members.first()); return }
        showDialog(AlertDialog.Builder(this).setTitle("选择成员")
            .setItems(members.map { name(it) }.toTypedArray()) { _, i -> manage(members[i]) }
            .setNegativeButton("关闭",null).create())
    }

    private fun previewWriteBack(share: JSONObject, preview: JSONObject) {
        val folder = share.optString("name")
        val changes = mapOf("added" to "新增", "modified" to "修改", "deleted" to "删除")
        val detail = buildString {
            append("确认后，今后在本机的修改和删除会同步至“$folder”。\n\n")
            append("本次将回传：新增 ${preview.getInt("added")} 项，修改 ${preview.getInt("modified")} 项，删除 ${preview.getInt("deleted")} 项。")
            val paths = preview.optJSONArray("paths")?.objects().orEmpty()
            for (file in paths) append("\n${changes[file.optString("change")].orEmpty()}  ${file.optString("path")}")
            val total = preview.getInt("added") + preview.getInt("modified") + preview.getInt("deleted")
            if (total > paths.size) append("\n…另有 ${total - paths.size} 项")
        }
        confirmOperation("核对回传内容",detail,"确认开启回传","正在确认…",{
            request("POST","/api/shares/${Uri.encode(share.getString("id"))}/writeback-confirm",
                JSONObject().put("token",preview.getString("token")))
        },"已开启回传")
    }

    private fun workspaceInvite(candidates: List<JSONObject>, share: JSONObject?) {
        selectPeer(candidates) { peer ->
            val name = candidates.first { it.optString("id") == peer }.optString("name")
            val fields = column().apply { setPadding(24.dp(),12.dp(),24.dp(),0) }
            val input = EditText(this).apply { hint = "工作区名称"; setSingleLine() }
            if (share == null) fields.addView(input)
            val roles = RadioGroup(this)
            val collaborator = RadioButton(this).apply { id = View.generateViewId(); text = "共同编辑" }
            val follower = RadioButton(this).apply { id = View.generateViewId(); text = "跟随此文件夹" }
            roles.addView(collaborator); roles.addView(follower); roles.check(collaborator.id); fields.addView(roles)
            val back = CheckBox(this).apply { text = "允许这台设备回传修改"; visibility = View.GONE }
            val hint = text("双方修改的文件会同步给其他成员。",13,design.muted)
            val error = text("",13,design.ink).apply { visibility = View.GONE; accessibilityLiveRegion = View.ACCESSIBILITY_LIVE_REGION_POLITE }
            fields.addView(back); fields.addView(hint); fields.addView(error)
            roles.setOnCheckedChangeListener { _, id ->
                back.visibility = if (id == follower.id) View.VISIBLE else View.GONE
                hint.text = if (id == follower.id) "此设备接收本机的文件副本。开启回传后，它的修改也会同步回来。" else "双方的修改会同步给其他成员。"
            }
            val current = AlertDialog.Builder(this).setTitle("邀请$name").setView(ScrollView(this).apply { addView(fields) })
                .setNegativeButton("取消",null).setPositiveButton("发送邀请",null).create()
            showDialog(current)
            current.getButton(AlertDialog.BUTTON_POSITIVE).setOnClickListener {
                val folder = input.text.toString().trim()
                if (share == null && folder.isEmpty()) { input.error="请输入工作区名称"; return@setOnClickListener }
                val role = if (roles.checkedRadioButtonId == follower.id) "follower" else "collaborator"
                val writeBack = role == "follower" && back.isChecked
                submitDialog(current, "正在发送…", {
                    if (share == null) engine().createWorkspaceWithRole(folder, peer, "", role, writeBack)
                    else request("POST", "/api/shares/${share.getString("id")}/members", JSONObject()
                        .put("deviceId",peer).put("role",role).put("allowWriteBack",writeBack))
                }, { reason -> error.text = reason; error.visibility = View.VISIBLE }) { message("同步邀请已发送") }
            }
        }
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
                    if (workspace.isNotEmpty()) {
                        engine().importToWorkspace(workspace, imported.name, name)
                        try { request("POST","/api/shares/$workspace/sync"); "文件已导入，正在同步" }
                        catch (_: Exception) { "文件已保存在工作区，可在设备在线后点击立即同步" }
                    }
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
