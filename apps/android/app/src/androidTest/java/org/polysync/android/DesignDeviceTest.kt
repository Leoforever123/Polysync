package org.polysync.android

import android.app.AlertDialog
import android.view.View
import android.view.ViewGroup
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
                assertFalse(current.isShowing)
            } finally { snapshot.set(screen,original); display() }
        }
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
                assertTrue(textIn(dialog().window!!.decorView,"停止与此设备同步")); dialog().dismiss()
            } finally { snapshot.set(screen,original); display() }
        }
    }
}
