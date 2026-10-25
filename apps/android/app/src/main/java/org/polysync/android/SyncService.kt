package org.polysync.android

import android.app.*
import android.content.Intent
import android.os.*
import org.polysync.core.mobile.Engine
import org.polysync.core.mobile.Mobile
import android.content.Context
import java.util.concurrent.Executors

class SyncService : Service() {
    companion object {
        @Volatile var engine: Engine? = null
        @Volatile var error = ""
        @Volatile var visible = false
        @Volatile var discoveryStatus = "正在启动自动发现"
        val discoveryWorker = Executors.newSingleThreadExecutor()
        val worker = Executors.newFixedThreadPool(3)
        private val lifecycle = Executors.newSingleThreadExecutor()
        val lifecycleLock = Any()
        val background = BackgroundPolicy({ SystemClock.elapsedRealtime() })
        fun userStopped(context: Context) = context.getSharedPreferences("service", Context.MODE_PRIVATE).getBoolean("userStopped", false)
        fun setUserStopped(context: Context, stopped: Boolean) {
            context.getSharedPreferences("service", Context.MODE_PRIVATE).edit().putBoolean("userStopped", stopped).apply()
        }
    }
    private val main = Handler(Looper.getMainLooper())
    private var nearby: NearbyDiscovery? = null
    @Volatile private var destroyed = false
    private val watchdog = object : Runnable {
        override fun run() {
            if (destroyed) return
            worker.execute {
                // Failure must not be interpreted as idle and interrupt work.
                val active = try { engine?.hasActiveWork() ?: true } catch (_: Exception) { true }
                main.post {
                    if (destroyed) return@post
                    if (background.shouldStop(visible, active)) stopSelf()
                    else main.postDelayed(this, 10_000)
                }
            }
        }
    }
    override fun onCreate() {
        super.onCreate()
        background.touch()
        val manager = getSystemService(NotificationManager::class.java)
        manager.createNotificationChannel(NotificationChannel("sync", "同步与传输", NotificationManager.IMPORTANCE_LOW))
        val open = PendingIntent.getActivity(this, 0, Intent(this, MainActivity::class.java), PendingIntent.FLAG_IMMUTABLE)
        val stop = PendingIntent.getService(this, 1, Intent(this, SyncService::class.java).setAction("stop"), PendingIntent.FLAG_IMMUTABLE)
        startForeground(1, Notification.Builder(this, "sync").setContentTitle("PolySync 可接收文件")
            .setContentText("后台空闲 10 分钟后停止；点击打开")
            .setSmallIcon(android.R.drawable.stat_sys_upload_done).setContentIntent(open)
            .addAction(Notification.Action.Builder(null, "停止", stop).build()).setOngoing(true).build())
        lifecycle.execute {
            try {
                if (destroyed) return@execute
                val next = Mobile.newEngine()
                next.start(filesDir.absolutePath + "/polysync", Build.MODEL, ":45123")
                synchronized(lifecycleLock) {
                    if (destroyed) next.stop() else { engine = next; error = "" }
                }
                if (!destroyed) {
                    val identity = next.discoveryInfo()
                    main.post {
                        if (!destroyed) {
                            nearby = NearbyDiscovery(this, next) { discoveryStatus = it }.also { it.start(identity) }
                        }
                    }
                }
            } catch (e: Exception) { error = e.message ?: "服务启动失败"; main.post { stopSelf() } }
        }
        main.postDelayed(watchdog, 10_000)
    }
    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent?.action == "stop") { setUserStopped(this, true); stopSelf(); return START_NOT_STICKY }
        setUserStopped(this, false)
        if (intent?.action == "rediscover") {
            val current = engine
            if (current != null) worker.execute {
                try {
                    val identity = current.discoveryInfo()
                    main.post { if (!destroyed) { nearby?.close(); nearby = NearbyDiscovery(this, current) { discoveryStatus = it }.also { it.start(identity) } } }
                } catch (_: Exception) { }
            }
        }
        return START_NOT_STICKY
    }
    override fun onTimeout(startId: Int, fgsType: Int) { stopSelf() }
    override fun onBind(intent: Intent?) = null
    override fun onDestroy() {
        destroyed = true
        nearby?.close(); nearby = null; discoveryStatus = "服务已停止"
        main.removeCallbacksAndMessages(null)
        val old = synchronized(lifecycleLock) { val value = engine; engine = null; value }
        // Cancellation must not wait behind a blocked file staging operation.
        lifecycle.execute { old?.stop() }
        stopForeground(STOP_FOREGROUND_REMOVE)
        super.onDestroy()
    }
}
