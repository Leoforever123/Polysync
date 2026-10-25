package org.polysync.android

import android.content.Context
import android.net.nsd.NsdManager
import android.net.nsd.NsdServiceInfo
import android.os.Build
import android.os.Handler
import android.os.Looper
import org.json.JSONArray
import org.json.JSONObject
import org.polysync.core.mobile.Engine
import java.util.ArrayDeque

/** All state changes run on the main looper; socket work belongs to Android NSD. */
class NearbyDiscovery(context: Context, private val engine: Engine, private val status: (String) -> Unit) {
    private val nsd = context.getSystemService(NsdManager::class.java)
    private val main = Handler(Looper.getMainLooper())
    @Volatile private var closed = false
    private var registering = false
    private var browsing = false
    private var resolving = false
    private var self = ""
    private var snapshotFailed = false
    private val pending = ArrayDeque<NsdServiceInfo>()
    private val present = mutableSetOf<String>()
    private val devices = linkedMapOf<String, JSONObject>()
    private fun key(info: NsdServiceInfo) = info.serviceName + if (Build.VERSION.SDK_INT >= 33) "@${info.network}" else ""
    private fun post(action: () -> Unit) { main.post { if (!closed) action() } }
    private fun publish() {
        val snapshot = JSONArray(devices.values.toList()).toString()
        // Capture each snapshot; a single worker preserves found/lost event order.
        SyncService.discoveryWorker.execute {
            try {
                engine.updateDiscovery(snapshot)
                post { if (snapshotFailed) { snapshotFailed = false; status("正在发现同一网络的设备") } }
            } catch (_: Exception) {
                post { snapshotFailed = true; status("部分发现记录无效或更新失败 · 可重新发现") }
            }
        }
    }
    private val registration = object : NsdManager.RegistrationListener {
        override fun onServiceRegistered(info: NsdServiceInfo) { if (closed) { try { nsd.unregisterService(this) } catch (_: Exception) {} } }
        override fun onRegistrationFailed(info: NsdServiceInfo, code: Int) = post { registering = false; status("广播暂不可用 · 可手动连接") }
        override fun onServiceUnregistered(info: NsdServiceInfo) { }
        override fun onUnregistrationFailed(info: NsdServiceInfo, code: Int) { android.util.Log.w("PolySyncNSD", "Unregister failed: $code") }
    }
    private val discovery = object : NsdManager.DiscoveryListener {
        override fun onDiscoveryStarted(type: String) = post { status("正在发现同一网络的设备") }
        override fun onServiceFound(info: NsdServiceInfo) = post {
            if (info.serviceType.trim('.') != "_polysync._tcp" || present.size >= 128) return@post
            if (present.add(key(info))) { pending.add(info); resolveNext() }
        }
        override fun onServiceLost(info: NsdServiceInfo) = post {
            present.remove(key(info)); devices.remove(key(info)); pending.removeAll { key(it) == key(info) }; publish()
        }
        override fun onDiscoveryStopped(type: String) { }
        override fun onStartDiscoveryFailed(type: String, code: Int) = post {
            try { nsd.stopServiceDiscovery(this) } catch (_: Exception) { }
            browsing = false; status("自动发现暂不可用 · 可手动连接")
        }
        override fun onStopDiscoveryFailed(type: String, code: Int) { android.util.Log.w("PolySyncNSD", "Stop discovery failed: $code") }
    }
    fun start(identity: String) {
        check(Looper.myLooper() == Looper.getMainLooper())
        val data = JSONObject(identity); self = data.getString("id")
        val info = NsdServiceInfo().apply {
            serviceName = "PolySync-${self.take(12)}"; serviceType = "_polysync._tcp."; port = data.getInt("port")
            for (field in listOf("id", "name", "key", "fp", "version")) setAttribute(field, data.getString(field))
        }
        try { registering = true; nsd.registerService(info, NsdManager.PROTOCOL_DNS_SD, registration) }
        catch (_: Exception) { registering = false; status("广播暂不可用 · 可手动连接") }
        try { browsing = true; nsd.discoverServices("_polysync._tcp.", NsdManager.PROTOCOL_DNS_SD, discovery) }
        catch (_: Exception) { browsing = false; status("自动发现暂不可用 · 可手动连接") }
    }
    @Suppress("DEPRECATION")
    private fun resolveNext() {
        if (closed || resolving || pending.isEmpty()) return
        val info = pending.removeFirst(); val serviceKey = key(info); resolving = true
        // Android 10 supports one outstanding legacy resolution; queue, don't race.
        val listener = object : NsdManager.ResolveListener {
            override fun onResolveFailed(service: NsdServiceInfo, code: Int) = post {
                resolving = false; present.remove(serviceKey); status("部分设备未解析 · 可重新发现"); resolveNext()
            }
            override fun onServiceResolved(service: NsdServiceInfo) = post {
                resolving = false
                if (present.contains(serviceKey)) {
                    val attrs = service.attributes.mapValues { it.value.toString(Charsets.UTF_8) }
                    val addresses = if (Build.VERSION.SDK_INT >= 34) service.hostAddresses else listOfNotNull(service.host)
                    val hosts = addresses.filter { !it.isAnyLocalAddress && !it.isMulticastAddress && !it.isLoopbackAddress }
                        .sortedBy { if (it is java.net.Inet4Address) 0 else 1 }
                        .mapNotNull { it.hostAddress }.map { if (it.contains(':')) "[$it]:${service.port}" else "$it:${service.port}" }
                    if (attrs["version"] == "2" && attrs["id"] != self && (attrs["id"]?.length ?: 0) in 1..128 && attrs["key"]?.matches(Regex("[A-Za-z0-9+/]{43}")) == true && hosts.isNotEmpty()) {
                        devices[serviceKey] = JSONObject().put("id", attrs["id"]).put("name", attrs["name"] ?: service.serviceName)
                            .put("publicKey", attrs["key"]).put("addresses", JSONArray(hosts))
                        publish()
                    }
                }
                resolveNext()
            }
        }
        try { nsd.resolveService(info, listener) } catch (_: Exception) { resolving = false; present.remove(serviceKey); resolveNext() }
    }
    fun close() {
        closed = true; pending.clear(); present.clear(); devices.clear(); publish()
        if (browsing) { try { nsd.stopServiceDiscovery(discovery) } catch (_: Exception) {} }
        if (registering) { try { nsd.unregisterService(registration) } catch (_: Exception) {} }
        browsing = false; registering = false
    }
}
