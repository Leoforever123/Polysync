package org.polysync.android

import android.test.InstrumentationTestRunner
import org.polysync.core.mobile.Mobile
import org.json.JSONObject
import java.io.File
import java.net.HttpURLConnection
import java.net.URI
import java.util.UUID

// Run separately with -e desktopAddress <LAN-IP:port> and -e desktopRoot <temporary path>.
// Only control calls use adb reverse; TLS file traffic must cross the real Wi-Fi LAN.
class DesktopInteropTest : ForegroundDeviceTest() {
    private fun desktop(method: String, path: String, body: JSONObject = JSONObject()): JSONObject {
        val connection = URI("http://127.0.0.1:45424$path").toURL().openConnection() as HttpURLConnection
        connection.requestMethod = method; connection.connectTimeout = 15000; connection.readTimeout = 15000
        try {
            if (method != "GET") { connection.doOutput = true; connection.setRequestProperty("Content-Type", "application/json"); connection.outputStream.use { it.write(body.toString().toByteArray()) } }
            val code = connection.responseCode
            val result = (if (code < 400) connection.inputStream else connection.errorStream).bufferedReader().use { it.readText() }
            assertTrue("Desktop API $code: $result", code < 400)
            return JSONObject(result)
        } finally { connection.disconnect() }
    }
    fun testPhoneAndWindowsOnLAN() {
        val args = (instrumentation as InstrumentationTestRunner).arguments
        val address = requireNotNull(args.getString("desktopAddress"))
        val desktopRoot = requireNotNull(args.getString("desktopRoot"))
        val dir = File(instrumentation.targetContext.filesDir, "lan-test-${UUID.randomUUID()}")
        val engine = Mobile.newEngine()
        try {
            engine.start(dir.absolutePath, "Android LAN test", ":45223")
            val session = JSONObject(engine.request("POST", "/api/pair/start", JSONObject().put("address", address).toString())).getString("sessionId")
            val pending = desktop("GET", "/api/status").getJSONArray("pairingRequests").getJSONObject(0)
            desktop("POST", "/api/pair/requests/${pending.getString("id")}/approve")
            val approved = desktop("GET", "/api/status")
            val code = approved.getJSONArray("pairingRequests").getJSONObject(0).getString("code")
            engine.request("POST", "/api/pair/confirm", JSONObject().put("sessionId", session).put("code", code).toString())
            val peer = approved.getJSONObject("device").getString("id")
            val payload = "Android to Windows over real Wi-Fi: 中文测试".repeat(10000).toByteArray()
            File(dir, "imports/payload").writeBytes(payload)
            engine.sendImport(peer, "payload", "android-lan.txt")
            waitUntil {
                val tasks = desktop("GET", "/api/transfers").getJSONArray("tasks")
                tasks.length() > 0 && tasks.getJSONObject(0).getString("state") == "complete"
            }
            val share = JSONObject(engine.createWorkspace("LAN test workspace", peer, "")).getString("id")
            engine.importToWorkspace(share, "payload", "phone.txt")
            val invitation = desktop("GET", "/api/status").getJSONArray("shareInvitations").getJSONObject(0).getString("id")
            desktop("POST", "/api/share-invitations/$invitation/accept", JSONObject().put("path", desktopRoot).put("autoSync", false).put("intervalSeconds", 30))
            waitUntil { try { File(engine.workspaceExportPath(share, "desktop.txt")).readText() == "Windows to Android over real Wi-Fi" } catch (_: Exception) { false } }
            val state = JSONObject(engine.request("GET", "/api/status", ""))
            assertEquals(1, state.getJSONArray("pairedDevices").length())
        } finally { engine.stop(); dir.deleteRecursively() }
    }
    private fun waitUntil(test: () -> Boolean) {
        val deadline = System.currentTimeMillis() + 30_000
        while (System.currentTimeMillis() < deadline) { if (test()) return; Thread.sleep(200) }
        fail("Timed out waiting for real LAN operation")
    }
}
