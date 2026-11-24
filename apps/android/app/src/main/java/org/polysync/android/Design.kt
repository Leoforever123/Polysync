package org.polysync.android

import android.content.Context
import android.content.res.ColorStateList
import android.graphics.*
import android.graphics.drawable.GradientDrawable
import android.graphics.drawable.RippleDrawable
import android.graphics.drawable.StateListDrawable
import android.view.View
import android.widget.*

/** Small native design system: no web engine, image assets or external fonts. */
class Design(c: Context) {
    val paper = c.getColor(R.color.ps_paper)
    val surface = c.getColor(R.color.ps_surface)
    val ink = c.getColor(R.color.ps_ink)
    val muted = c.getColor(R.color.ps_muted)
    val green = c.getColor(R.color.ps_accent)
    val mint = c.getColor(R.color.ps_mint)
    val line = c.getColor(R.color.ps_line)
    private val onAccent = c.getColor(R.color.ps_on_accent)
    fun dp(c: Context, n: Int) = (n * c.resources.displayMetrics.density).toInt()
    fun shape(c: Context, color: Int, radius: Int = 22, border: Int = 0) = GradientDrawable().apply {
        setColor(color); cornerRadius = dp(c, radius).toFloat()
        if (border != 0) setStroke(dp(c, 1), border)
    }
    fun text(c: Context, value: String, size: Int = 15, color: Int = ink, bold: Boolean = false) = TextView(c).apply {
        text = value; textSize = size.toFloat(); setTextColor(color); includeFontPadding = false
        setLineSpacing(dp(c, 4).toFloat(), 1f)
        if (bold) typeface = Typeface.create("sans-serif-medium", Typeface.NORMAL)
    }
    fun button(c: Context, value: String, primary: Boolean = false, action: () -> Unit) = Button(c).apply {
        text = value; textSize = 14f; isAllCaps = false; stateListAnimator = null
        typeface = Typeface.create("sans-serif-medium", Typeface.NORMAL)
        setTextColor(if (primary) onAccent else green)
        minimumHeight = dp(c, 48); minHeight = dp(c, 48); setPadding(dp(c, 16), dp(c, 10), dp(c, 16), dp(c, 10))
        val states = StateListDrawable().apply {
            addState(intArrayOf(android.R.attr.state_focused), shape(c, if(primary) green else mint, 16, if(primary) ink else green))
            addState(intArrayOf(), shape(c, if (primary) green else surface, 16, if (primary) 0 else line))
        }
        background = RippleDrawable(ColorStateList.valueOf((green and 0x00ffffff) or 0x33000000), states, shape(c, Color.WHITE, 16))
        setOnClickListener { action() }
    }
}

class Mark @JvmOverloads constructor(context: Context, private val kind: String = "brand", private val tint: Int = context.getColor(R.color.ps_accent)) : View(context) {
    private val paint = Paint(Paint.ANTI_ALIAS_FLAG).apply { style = Paint.Style.STROKE; strokeWidth = 1.7f; strokeCap = Paint.Cap.ROUND; strokeJoin = Paint.Join.ROUND }
    private val path = Path()
    private val centers = floatArrayOf(10f,19f,16f,11f,23f,19f)
    override fun onDraw(canvas: Canvas) {
        super.onDraw(canvas)
        canvas.save(); canvas.scale(width / 32f, height / 32f); paint.color = tint
        when (kind) {
            "send" -> { path.reset(); path.moveTo(5f, 14f); path.lineTo(27f, 5f); path.lineTo(20f, 27f); path.lineTo(14f, 19f); path.close(); canvas.drawPath(path, paint); canvas.drawLine(14f,19f,27f,5f,paint) }
            "folder" -> { path.reset(); path.moveTo(3f, 10f); path.lineTo(3f, 7f); path.lineTo(13f,7f); path.lineTo(16f,11f); path.lineTo(29f,11f); path.lineTo(27f,26f); path.lineTo(3f,26f); path.close(); canvas.drawPath(path,paint) }
            "devices" -> { canvas.drawRoundRect(3f,5f,23f,21f,2f,2f,paint); canvas.drawLine(8f,26f,20f,26f,paint); canvas.drawLine(14f,21f,14f,26f,paint); canvas.drawRoundRect(22f,13f,30f,28f,2f,2f,paint) }
            "phone" -> { canvas.drawRoundRect(8f,3f,24f,29f,4f,4f,paint); canvas.drawLine(14f,25f,18f,25f,paint) }
            "radar" -> { canvas.drawCircle(16f,16f,12f,paint); paint.alpha=100; canvas.drawCircle(16f,16f,7f,paint); paint.alpha=255; canvas.drawLine(16f,16f,24f,8f,paint); paint.style=Paint.Style.FILL; canvas.drawCircle(12f,19f,2f,paint); paint.style=Paint.Style.STROKE }
            else -> for (i in 0..2) { val x=centers[i*2];val y=centers[i*2+1];canvas.save(); canvas.rotate(45f,x,y); canvas.drawRoundRect(x-4.5f,y-4.5f,x+4.5f,y+4.5f,2f,2f,paint); canvas.restore() }
        }
        canvas.restore()
    }
}
