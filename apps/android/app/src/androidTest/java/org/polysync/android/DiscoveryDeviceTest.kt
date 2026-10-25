package org.polysync.android

import android.test.InstrumentationTestRunner
import org.json.JSONObject
import java.net.URI

class DiscoveryDeviceTest : ForegroundDeviceTest() {
    private fun desktop(): JSONObject = URI("http://127.0.0.1:45424/api/status").toURL().openConnection().apply { connectTimeout=5000;readTimeout=5000 }.getInputStream().bufferedReader().use { JSONObject(it.readText()) }
    fun testBidirectionalMdnsWithWindows() {
        val expected=(instrumentation as InstrumentationTestRunner).arguments.getString("desktopId") ?: desktop().getJSONObject("device").getString("id")
        val deadline=System.currentTimeMillis()+60000
        var phoneSeesDesktop=false;var desktopSeesPhone=false
        while(System.currentTimeMillis()<deadline) {
            val engine=SyncService.engine
            if(engine!=null) {
                val phone=JSONObject(engine.request("GET","/api/status",""))
                val nearby=phone.optJSONArray("nearbyDevices")
                phoneSeesDesktop=nearby!=null && (0 until nearby.length()).any{nearby.getJSONObject(it).getString("id")==expected}
                val phoneId=phone.getJSONObject("device").getString("id")
                val other=desktop().optJSONArray("nearbyDevices")
                desktopSeesPhone=other!=null && (0 until other.length()).any{other.getJSONObject(it).getString("id")==phoneId}
                if(phoneSeesDesktop && desktopSeesPhone) return
            }
            Thread.sleep(500)
        }
        fail("mDNS discovery phone→desktop=$phoneSeesDesktop desktop→phone=$desktopSeesPhone; ${SyncService.discoveryStatus}")
    }
}
