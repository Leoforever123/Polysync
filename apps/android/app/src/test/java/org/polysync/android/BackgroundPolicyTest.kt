package org.polysync.android
import org.junit.Assert.*
import org.junit.Test
class BackgroundPolicyTest {
    @Test fun queuedAndRunningImportsProtectIdleService() {
        var time = 0L
        val policy = BackgroundPolicy({ time }, 100)
        policy.beginWork(); policy.beginWork()
        time = 1000; assertFalse(policy.shouldStop(false, false))
        policy.endWork(); time = 2000; assertFalse(policy.shouldStop(false, false))
        policy.endWork(); time = 2050; assertFalse(policy.shouldStop(false, false))
        time = 2101; assertTrue(policy.shouldStop(false, false))
    }
    @Test fun foregroundAndCoreWorkResetIdleDeadline() {
        var time = 0L
        val policy = BackgroundPolicy({ time }, 100)
        time = 1000; assertFalse(policy.shouldStop(true, false))
        time = 2000; assertFalse(policy.shouldStop(false, true))
        time = 2100; assertFalse(policy.shouldStop(false, false))
        time = 2101; assertTrue(policy.shouldStop(false, false))
        policy.touch(); assertFalse(policy.shouldStop(false, false))
    }
}
