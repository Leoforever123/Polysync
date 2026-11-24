package org.polysync.android

import android.content.Intent
import android.test.InstrumentationTestCase
import java.net.InetAddress
import java.net.ServerSocket

class AppLifecycleTest : InstrumentationTestCase() {
    fun testForegroundServiceStopsReleasesPortAndRestarts() {
        val context = instrumentation.targetContext
        val activity = instrumentation.startActivitySync(Intent(context, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
        try {
            waitUntil { SyncService.engine != null }
            repeat(3) {
                instrumentation.runOnMainSync { context.stopService(Intent(context, SyncService::class.java)) }
                waitUntil { SyncService.engine == null && portFree() }
                instrumentation.runOnMainSync { context.startForegroundService(Intent(context, SyncService::class.java)) }
                waitUntil { SyncService.engine != null }
            }
        } finally {
            instrumentation.runOnMainSync { context.stopService(Intent(context, SyncService::class.java)); activity.finish() }
        }
    }
    fun testUserStopSurvivesActivityRecreationAndExplicitRestart() {
        val context = instrumentation.targetContext
        val previous = SyncService.userStopped(context)
        var activity: android.app.Activity? = null
        try {
            instrumentation.runOnMainSync { SyncService.setUserStopped(context, false) }
            activity = instrumentation.startActivitySync(Intent(context, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
            waitUntil { SyncService.engine != null }
            instrumentation.runOnMainSync { context.startService(Intent(context, SyncService::class.java).setAction("stop")) }
            waitUntil { SyncService.userStopped(context) && SyncService.engine == null && portFree() }
            val monitor = instrumentation.addMonitor(MainActivity::class.java.name, null, false)
            try {
                instrumentation.runOnMainSync { activity!!.recreate() }
                val recreated = instrumentation.waitForMonitorWithTimeout(monitor, 10_000)
                assertNotNull("Activity did not recreate", recreated)
                activity = recreated
                instrumentation.waitForIdleSync()
                Thread.sleep(500)
                assertTrue(SyncService.userStopped(context))
                assertNull("Recreation restarted user-stopped service", SyncService.engine)
                assertTrue(portFree())
            } finally { instrumentation.removeMonitor(monitor) }
            instrumentation.runOnMainSync { context.startForegroundService(Intent(context, SyncService::class.java)) }
            waitUntil { SyncService.engine != null && !SyncService.userStopped(context) }
        } finally {
            instrumentation.runOnMainSync {
                context.stopService(Intent(context, SyncService::class.java))
                SyncService.setUserStopped(context, previous)
                activity?.finish()
            }
        }
    }
    private fun portFree(): Boolean = try { ServerSocket(SyncService.LISTEN_PORT, 1, InetAddress.getByName("127.0.0.1")).use { }; true } catch (_: Exception) { false }
    private fun waitUntil(test: () -> Boolean) {
        val deadline = System.currentTimeMillis() + 15_000
        while (System.currentTimeMillis() < deadline) { if (test()) return; Thread.sleep(100) }
        fail("Service lifecycle timed out: ${SyncService.error}")
    }
}
