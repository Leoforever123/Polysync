package org.polysync.android

import android.view.View
import android.view.ViewGroup
import android.widget.EditText
import android.widget.TextView
import android.app.AlertDialog
import org.json.JSONObject

class DesignDeviceTest : ForegroundDeviceTest() {
    private fun all(v: View): List<View> = listOf(v) + if(v is ViewGroup) (0 until v.childCount).flatMap { all(v.getChildAt(it)) } else emptyList()
    private fun texts(): List<String> { var result=listOf<String>(); instrumentation.runOnMainSync { result=all(activity!!.window.decorView).filterIsInstance<TextView>().map{it.text.toString()} };return result }
    private fun click(tag: String) { instrumentation.runOnMainSync { requireNotNull(activity!!.findViewById<ViewGroup>(android.R.id.content).findViewWithTag<View>(tag)).performClick() };instrumentation.waitForIdleSync() }
    fun testNavigationKeepsNativeStructureAndEmptyStates() {
        click("nav:随传");assertTrue(texts().contains("随手一传"));assertTrue(texts().contains("第一份文件，从这里出发"))
        click("nav:工作区");assertTrue(texts().contains("每一份，都同步。"));assertTrue(texts().contains("给文件一个共同的家"))
        click("nav:设备");assertTrue(texts().contains("附近设备"))
        click("nav:随传"); Thread.sleep(2600)
        val root=activity!!.window.decorView
        var before: View?=null
        instrumentation.runOnMainSync { before=root.findViewWithTag("选择文件发送") }
        Thread.sleep(2400)
        instrumentation.runOnMainSync { assertNotNull(before); assertSame("Polling rebuilt an unchanged button",before,root.findViewWithTag<View>("选择文件发送")) }
        // Display-only fixture: never add or remove trust in the user's engine.
        click("nav:设备")
        instrumentation.runOnMainSync {
            val screen = activity as MainActivity
            val snapshotField = MainActivity::class.java.getDeclaredField("snapshot").apply { isAccessible=true }
            val original = snapshotField.get(screen)
            try {
                snapshotField.set(screen, JSONObject("""{"pairedDevices":[{"id":"ui-unpair-fixture","name":"测试电脑","online":false}],"shares":[],"nearbyDevices":[]}"""))
                MainActivity::class.java.getDeclaredMethod("display").apply { isAccessible=true }.invoke(screen)
                val trigger = screen.findViewById<ViewGroup>(android.R.id.content).findViewWithTag<View>("unpair:ui-unpair-fixture")
                assertNotNull("Unpair action is missing", trigger)
                trigger.performClick()
                val current = MainActivity::class.java.getDeclaredField("dialog").apply { isAccessible=true }.get(screen) as AlertDialog
                val message = current.findViewById<TextView>(android.R.id.message).text.toString()
                assertTrue(message.contains("对方需单独取消配对"))
                assertTrue(message.contains("查看与导出"))
                assertEquals("保留配对", current.getButton(AlertDialog.BUTTON_NEGATIVE).text.toString())
                current.getButton(AlertDialog.BUTTON_NEGATIVE).performClick()
                assertFalse(current.isShowing)
            } finally { snapshotField.set(screen, original) }
        }
    }
}
