package org.polysync.android

import org.polysync.core.mobile.Mobile
import org.json.JSONObject
import org.json.JSONArray
import java.io.File
import java.util.UUID

// Runs the real arm64 Go core on the phone, using only isolated app-private data.
class CoreDeviceTest : ForegroundDeviceTest() {
    fun testPairTransferAndWorkspaceOnDevice() {
        val dir = File(instrumentation.targetContext.filesDir, "core-test-${UUID.randomUUID()}")
        val a = Mobile.newEngine(); val b = Mobile.newEngine()
        val left = File(dir, "left"); val right = File(dir, "right")
        try {
            a.start(left.absolutePath, "Phone A test", "127.0.0.1:0")
            b.start(right.absolutePath, "Phone B test", "127.0.0.1:0")
            val bState = JSONObject(b.request("GET", "/api/status", ""))
            val port = bState.getJSONObject("device").getInt("listenPort")
            val peer = bState.getJSONObject("device").getString("id")
            val session = JSONObject(a.request("POST", "/api/pair/start", JSONObject().put("address", "127.0.0.1:$port").toString())).getString("sessionId")
            val pending = JSONObject(b.request("GET", "/api/status", "")).getJSONArray("pairingRequests").getJSONObject(0)
            b.request("POST", "/api/pair/requests/${pending.getString("id")}/approve", "{}")
            val code = JSONObject(b.request("GET", "/api/status", "")).getJSONArray("pairingRequests").getJSONObject(0).getString("code")
            a.request("POST", "/api/pair/confirm", JSONObject().put("sessionId", session).put("code", code).toString())
            val payload = ByteArray(2 * 1024 * 1024) { (it % 251).toByte() }
            File(left, "imports/payload").writeBytes(payload)
            val id = a.sendImport(peer, "payload", "手机测试.bin")
            waitUntil {
                val tasks = JSONObject(b.request("GET", "/api/transfers", "")).getJSONArray("tasks")
                tasks.length() == 1 && tasks.getJSONObject(0).getString("state") == "complete"
            }
            val receipt = JSONObject(b.request("GET", "/api/transfers", "")).getJSONArray("tasks").getJSONObject(0)
            assertTrue(payload.contentEquals(File(b.exportPath(receipt.getString("id"), 0)).readBytes()))
            assertEquals(id, JSONObject(a.request("GET", "/api/transfers", "")).getJSONArray("tasks").getJSONObject(0).getString("id"))
            val share = JSONObject(a.createWorkspaceWithRole("测试工作区", peer, "", "follower", false)).getString("id")
            a.importToWorkspace(share, "payload", "文件.bin")
            val invitation = JSONObject(b.request("GET", "/api/status", "")).getJSONArray("shareInvitations").getJSONObject(0).getString("id")
            b.createWorkspace("测试工作区", "", invitation)
            waitUntil { JSONArray(b.workspaceFiles(share)).length() > 0 }
            assertTrue(payload.contentEquals(File(b.workspaceExportPath(share, "文件.bin")).readBytes()))
            // A second directory at the same path must not publish a deletion.
            val sourceState = { JSONObject(a.request("GET", "/api/status", "")).getJSONArray("shares").getJSONObject(0) }
            val source = File(sourceState().getString("path"))
            val originalExport = File(a.workspaceExportPath(share, "文件.bin"))
            val original = File(dir, "temporarily-disconnected")
            waitUntil { !a.hasActiveWork() && !b.hasActiveWork() }
            assertTrue(source.renameTo(original))
            assertTrue(source.mkdir())
            val wrongFile = File(source, "文件.bin").apply { writeText("replacement disk") }
            for ((action, operation) in listOf<Pair<String, () -> Unit>>(
                "list" to { a.workspaceFiles(share) },
                "import" to { a.importToWorkspace(share, "payload", "误导入.bin") },
                "export" to { a.workspaceExportPath(share, "文件.bin") }
            )) {
                try {
                    operation()
                    fail("Allowed $action on the replacement disk")
                } catch (e: Exception) {
                    assertTrue("$action did not report the missing original disk: ${e.message}", e.message.orEmpty().contains("原盘"))
                }
            }
            assertTrue(payload.contentEquals(originalExport.readBytes()))
            try {
                a.request("POST", "/api/shares/$share/sync", "{}")
                fail("A replacement folder accepted a manual sync")
            } catch (e: Exception) {
                assertTrue("Expected guidance to reconnect the disk: ${e.message}", e.message.orEmpty().contains("原盘"))
            }
            waitUntil { sourceState().getJSONObject("status").optString("state") == "paused" }
            assertEquals(listOf("文件.bin"),source.list().orEmpty().toList())
            assertEquals("replacement disk",wrongFile.readText())
            assertTrue(payload.contentEquals(File(b.workspaceExportPath(share, "文件.bin")).readBytes()))
            File(original, "reconnected.txt").writeText("original folder returned")
            assertTrue(wrongFile.delete())
            assertTrue(source.delete())
            assertTrue(original.renameTo(source))
            waitUntil {
                try { File(b.workspaceExportPath(share, "reconnected.txt")).readText() == "original folder returned" }
                catch (_: Exception) { false }
            }
            assertTrue(payload.contentEquals(File(b.workspaceExportPath(share, "文件.bin")).readBytes()))
            val info = JSONObject(b.request("GET","/api/status","")).getJSONArray("shares").getJSONObject(0)
            assertEquals("follower", info.getString("localRole"))
            File(right,"imports/local-only").writeText("keep local")
            try { b.importToWorkspace(share,"local-only","新增.txt"); fail("Read-only folder accepted import") } catch (_: Exception) { }
            try { b.workspaceExportPath(share, "../state/config.json"); fail("Accepted traversal") } catch (_: Exception) { }
            waitUntil { !a.hasActiveWork() && !b.hasActiveWork() }
            a.request("DELETE", "/api/paired-devices/$peer", "")
            val unpaired = JSONObject(a.request("GET", "/api/status", ""))
            assertEquals(0, unpaired.getJSONArray("pairedDevices").length())
            assertEquals("unpaired", unpaired.getJSONArray("shares").getJSONObject(0).getString("state"))
            assertTrue(payload.contentEquals(File(a.workspaceExportPath(share, "文件.bin")).readBytes()))
            try { a.request("POST", "/api/shares/$share/sync", "{}"); fail("Synced after unpair") } catch (_: Exception) { }
            a.stop()
            a.start(left.absolutePath, "Phone A test", "127.0.0.1:0")
            assertEquals(0, JSONObject(a.request("GET", "/api/status", "")).getJSONArray("pairedDevices").length())
        } finally { a.stop(); b.stop(); dir.deleteRecursively() }
    }
    fun testIdentitySurvivesTenRestarts() {
        val dir = File(instrumentation.targetContext.filesDir, "lifecycle-test-${UUID.randomUUID()}")
        val engine = Mobile.newEngine(); var identity = ""
        try {
            repeat(10) {
                engine.start(dir.absolutePath, "Lifecycle test", "127.0.0.1:0")
                val id = JSONObject(engine.request("GET", "/api/status", "")).getJSONObject("device").getString("id")
                if (identity.isEmpty()) identity = id else assertEquals(identity, id)
                engine.stop()
            }
        } finally { engine.stop(); dir.deleteRecursively() }
    }
    private fun waitUntil(test: () -> Boolean) {
        val deadline = System.currentTimeMillis() + 15_000
        while (System.currentTimeMillis() < deadline) { if (test()) return; Thread.sleep(100) }
        fail("Timed out waiting for real transfer")
    }
}
