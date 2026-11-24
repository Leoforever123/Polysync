package org.polysync.android

import android.app.AlertDialog
import android.view.View
import android.view.ViewGroup
import android.view.FrameMetrics
import android.view.Window
import android.os.Handler
import android.os.HandlerThread
import java.util.Collections
import android.widget.*
import org.json.JSONArray
import org.json.JSONObject

class DesignDeviceTest : ForegroundDeviceTest() {
    private val screen get() = activity as MainActivity
    private fun all(v: View): List<View> = listOf(v) + if (v is ViewGroup) (0 until v.childCount).flatMap { all(v.getChildAt(it)) } else emptyList()
    private fun field(name: String) = MainActivity::class.java.getDeclaredField(name).apply { isAccessible = true }
    private fun display() { MainActivity::class.java.getDeclaredMethod("display").apply { isAccessible = true }.invoke(screen) }
    private fun tagged(id: String) = screen.window.decorView.findViewWithTag<View>(id)
    private fun dialog() = field("dialog").get(screen) as AlertDialog
    private fun textIn(root: View, value: String) = all(root).filterIsInstance<TextView>().any { it.text.toString().contains(value) }
    private fun onScreen(check: () -> Unit) {
        var failure: Throwable? = null
        instrumentation.runOnMainSync { try { check() } catch (error: Throwable) { failure = error } }
        failure?.let { throw it }
    }
    override fun setUp() {
        super.setUp()
        val until = android.os.SystemClock.elapsedRealtime() + 15000
        while (SyncService.engine == null && android.os.SystemClock.elapsedRealtime() < until) Thread.sleep(50)
        assertNotNull("Service is required for these interactions", SyncService.engine)
    }

    fun testNavigationKeepsNativeStructureAndEmptyStates() {
        lateinit var unpair: AlertDialog
        onScreen {
            val snapshot = field("snapshot")
            val original = snapshot.get(screen)
            try {
                snapshot.set(screen, JSONObject())
                tagged("nav:随传").performClick()
                assertNotNull(tagged("send-pair"))
                assertNull(tagged("send-file"))
                tagged("send-pair").performClick()
                assertNotNull(tagged("manual-pair"))
                tagged("manual-pair").performClick()
                val form = dialog()
                val input = all(form.window!!.decorView).filterIsInstance<EditText>().single()
                form.getButton(AlertDialog.BUTTON_POSITIVE).performClick()
                assertNotNull(input.error)
                assertTrue(form.isShowing)
                form.dismiss()
                tagged("nav:工作区").performClick()
                assertNotNull(tagged("workspace-pair"))
                tagged("nav:设备").performClick()
                snapshot.set(screen, JSONObject("""{"pairedDevices":[{"id":"ui-peer","name":"测试电脑","online":false}]}"""))
                display()
                tagged("unpair:ui-peer").performClick()
                val current = dialog()
                assertTrue(textIn(current.window!!.decorView,"对方需单独取消配对"))
                assertTrue(textIn(current.window!!.decorView,"查看与导出"))
                current.getButton(AlertDialog.BUTTON_NEGATIVE).performClick()
                unpair = current
            } finally { snapshot.set(screen,original); display() }
        }
        instrumentation.waitForIdleSync()
        onScreen { assertFalse(unpair.isShowing) }
    }

    fun testTransferHistoryKeepsRowsDuringProgressAndPrepend() {
        onScreen {
            val data = field("transfers")
            val old = data.get(screen)
            val tasks = JSONArray()
            fun task(n: Int) = JSONObject().put("id","t$n").put("state","sending").put("total",100).put("done",10)
                .put("direction","send").put("files",JSONArray().put(JSONObject().put("name","文档$n.txt")))
            repeat(1000) { tasks.put(task(it)) }
            try {
                data.set(screen,tasks)
                tagged("nav:随传").performClick()
                val first = tagged("transfer:t0")
                val cancel = tagged("cancel:t0")
                assertNotNull(first); assertNotNull(cancel)
                assertNull(tagged("transfer:t20"))
                tasks.getJSONObject(0).put("done",60)
                display()
                assertSame(first,tagged("transfer:t0"))
                assertSame(cancel,tagged("cancel:t0"))
                val changed = JSONArray().put(task(1001))
                for (i in 0 until tasks.length()) changed.put(tasks.getJSONObject(i))
                data.set(screen,changed); display()
                assertSame("An incoming task replaced a live control",cancel,tagged("cancel:t0"))
                assertTrue(textIn(screen.window.decorView,"显示更多"))
                assertNull(tagged("transfer:t19"))
                tagged("nav:工作区").performClick()
                assertNull(tagged("transfer:t0"))
            } finally { data.set(screen,old); display() }
        }
    }

    fun testLongHistoryScrollMeasurement() {
        val old = field("transfers").get(screen)
        val frames = Collections.synchronizedList(mutableListOf<Pair<Long,Long>>())
        val frameThread = HandlerThread("PolysyncFrameMetrics").apply { start() }
        val observer = Window.OnFrameMetricsAvailableListener { _, frame, _ ->
            frames.add(frame.getMetric(FrameMetrics.INTENDED_VSYNC_TIMESTAMP) to frame.getMetric(FrameMetrics.TOTAL_DURATION))
        }
        val scroll = field("scroll").get(screen) as ScrollView
        try {
            onScreen {
                // Keep the synthetic history in place while real platform rendering is measured.
                field("resumed").setBoolean(screen,false)
                val tasks = JSONArray()
                repeat(1000) { i ->
                    tasks.put(JSONObject().put("id","s$i").put("state","complete").put("direction","send").put("total",4096)
                        .put("files",JSONArray().put(JSONObject().put("name","工作记录$i.txt"))))
                }
                field("transfers").set(screen,tasks)
                tagged("nav:随传").performClick()
            }
            instrumentation.waitForIdleSync()
            Thread.sleep(400)
            onScreen { screen.window.addOnFrameMetricsAvailableListener(observer,Handler(frameThread.looper)); scroll.fling(4800) }
            Thread.sleep(650)
            onScreen { scroll.fling(-4800) }
            Thread.sleep(650)
            onScreen { screen.window.removeOnFrameMetricsAvailableListener(observer) }
            val samples = synchronized(frames) { frames.toList() }
            val sorted = samples.map { it.second }.sorted()
            assertTrue("Expected scrolling frames", sorted.size >= 10)
            val p95 = sorted[((sorted.size - 1)*.95).toInt()] / 1_000_000.0
            val gaps = samples.zipWithNext { a,b -> b.first - a.first }.sorted()
            val p95Interval = gaps[((gaps.size - 1)*.95).toInt()] / 1_000_000.0
            val over25 = gaps.count { it > 25_000_000L }
            android.util.Log.i("PolySyncTiming", "1000 tasks / 20 rows, frames=${sorted.size} p95RenderMs=$p95 p95IntervalMs=$p95Interval gapsOver25ms=$over25")
        } finally {
            frameThread.quitSafely()
            onScreen { field("transfers").set(screen,old); field("resumed").setBoolean(screen,true); display() }
        }
    }

    fun testSharingRolesStayWithTheFolder() {
        onScreen {
            val snapshot = field("snapshot")
            val original = snapshot.get(screen)
            try {
                val state = JSONObject("""{"pairedDevices":[{"id":"a","name":"电脑 A"},{"id":"b","name":"电脑 B"}],"shares":[{"id":"w","name":"文稿","state":"active","localRole":"collaborator","members":[{"deviceId":"a","role":"collaborator","state":"active"}]}]}""")
                snapshot.set(screen,state)
                tagged("nav:工作区").performClick()
                tagged("member:w").performClick()
                val form = dialog()
                val content = form.window!!.decorView
                val back = all(content).filterIsInstance<CheckBox>().single()
                assertEquals(View.GONE,back.visibility)
                all(content).filterIsInstance<RadioButton>().first { it.text == "跟随此文件夹" }.performClick()
                assertEquals(View.VISIBLE,back.visibility); assertFalse(back.isChecked)
                form.dismiss()
                val share = state.getJSONArray("shares").getJSONObject(0)
                share.put("localRole","follower")
                display()
                assertNull(tagged("member:w")); assertNull(tagged("import:w"))
                assertNotNull(tagged("files:w"))
                assertTrue(textIn(screen.window.decorView,"本机只接收副本"))
                share.getJSONArray("members").getJSONObject(0).put("allowWriteBack",true)
                display()
                assertNotNull(tagged("import:w"))
                share.getJSONArray("members").getJSONObject(0).put("writeBackPending",true)
                display()
                assertNotNull(tagged("preview:w")); assertNull(tagged("import:w"))
                tagged("manage:w").performClick()
                assertEquals("停止与此设备同步",dialog().listView.adapter.getItem(0).toString()); dialog().dismiss()
                share.put("state","unpaired").put("localRole","collaborator").put("peerDeviceId","a").put("members",JSONArray())
                state.getJSONArray("pairedDevices").remove(1)
                display()
                assertNotNull("A newly paired device can be invited to the retained folder",tagged("member:w"))
                assertNull(tagged("sync:w")); assertNotNull(tagged("files:w"))
            } finally { snapshot.set(screen,original); display() }
        }
    }
}
