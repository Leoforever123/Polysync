package org.polysync.android

import android.app.Activity
import android.content.Intent
import android.test.InstrumentationTestCase

// Vivo can freeze a headless instrumentation process between assertions. Keep
// the real app visible while isolated engines exercise the protocol on-device.
abstract class ForegroundDeviceTest : InstrumentationTestCase() {
    protected var activity: Activity? = null
    override fun setUp() {
        super.setUp()
        val context = instrumentation.targetContext
        activity = instrumentation.startActivitySync(Intent(context, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
        instrumentation.waitForIdleSync()
    }
    override fun tearDown() {
        instrumentation.runOnMainSync {
            instrumentation.targetContext.stopService(Intent(instrumentation.targetContext, SyncService::class.java))
            activity?.finish()
        }
        super.tearDown()
    }
}
