package com.eschgi.share.platform

import android.Manifest
import android.app.Activity
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.content.res.ColorStateList
import android.content.res.Configuration
import android.content.res.Resources
import android.graphics.Canvas
import android.graphics.Color
import android.graphics.Paint
import android.graphics.Path
import android.graphics.RectF
import android.graphics.drawable.GradientDrawable
import android.graphics.drawable.RippleDrawable
import android.os.Bundle
import android.util.Log
import android.util.Size
import android.util.TypedValue
import android.view.Gravity
import android.view.View
import android.view.ViewGroup.LayoutParams.MATCH_PARENT
import android.view.ViewGroup.LayoutParams.WRAP_CONTENT
import android.widget.FrameLayout
import android.widget.ImageButton
import android.widget.TextView
import androidx.activity.ComponentActivity
import androidx.activity.SystemBarStyle
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.camera.core.CameraSelector
import androidx.camera.core.CameraState
import androidx.camera.core.ImageAnalysis
import androidx.camera.core.ImageProxy
import androidx.camera.core.TorchState
import androidx.camera.core.resolutionselector.ResolutionSelector
import androidx.camera.core.resolutionselector.ResolutionStrategy
import androidx.camera.view.CameraController
import androidx.camera.view.LifecycleCameraController
import androidx.camera.view.PreviewView
import androidx.core.content.ContextCompat
import androidx.core.view.ViewCompat
import androidx.core.view.WindowInsetsCompat
import androidx.core.view.isVisible
import com.eschgi.share.R
import java.util.Locale
import java.util.concurrent.Executors

/**
 * Share's own QR code scanner: the camera through CameraX, the codes read by ZXing, both in the
 * app. Google's code scanner, which Share handed scanning to before, didn't open from Share on a
 * Pixel whose own QR scanner, the same one, worked (issue #5), and it needs Google Play services;
 * this one works alike on every phone with a camera, and asks for the camera the first time.
 *
 * Answers RESULT_OK with the first QR code's text in [EXTRA_TEXT], RESULT_CANCELED when closed, or
 * [RESULT_PROBLEM] with why it couldn't scan in [EXTRA_PROBLEM]: [PROBLEM_PERMISSION] when Share
 * may not use the camera, [PROBLEM_NO_CAMERA] without one, [PROBLEM_CAMERA] when it failed.
 */
class ScanActivity : ComponentActivity() {
    private val analysis = Executors.newSingleThreadExecutor()
    private val decoder = QrDecoder()
    private lateinit var preview: PreviewView
    private lateinit var light: ImageButton
    private val lightShape = GradientDrawable()
    private var lightOn = ""
    private var lightOff = ""

    /** A code was read: later frames are dropped. */
    @Volatile
    private var found = false

    private val permission = registerForActivityResult(ActivityResultContracts.RequestPermission()) { granted ->
        if (granted) start() else fail(PROBLEM_PERMISSION)
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        enableEdgeToEdge(SystemBarStyle.dark(Color.TRANSPARENT), SystemBarStyle.dark(Color.TRANSPARENT))
        super.onCreate(savedInstanceState)
        setContentView(layout(texts()))
        when {
            ContextCompat.checkSelfPermission(this, Manifest.permission.CAMERA) == PackageManager.PERMISSION_GRANTED -> start()
            // Turned while Android asked: the answer comes to the callback all the same.
            savedInstanceState == null -> permission.launch(Manifest.permission.CAMERA)
        }
    }

    override fun onDestroy() {
        super.onDestroy()
        analysis.shutdown()
    }

    /** Opens the back camera, else the front one, and reads its frames until one holds a QR code. */
    private fun start() {
        val camera = LifecycleCameraController(this).apply {
            setEnabledUseCases(CameraController.IMAGE_ANALYSIS)
            imageAnalysisBackpressureStrategy = ImageAnalysis.STRATEGY_KEEP_ONLY_LATEST
            // Enough pixels for a code an arm's length away; ZXing reads such a frame in a few ms.
            imageAnalysisResolutionSelector = ResolutionSelector.Builder()
                .setResolutionStrategy(ResolutionStrategy(Size(1280, 720), ResolutionStrategy.FALLBACK_RULE_CLOSEST_HIGHER_THEN_LOWER))
                .build()
            setImageAnalysisAnalyzer(analysis, ::read)
        }
        val ready = camera.initializationFuture
        ready.addListener({
            if (isFinishing || isDestroyed) return@addListener
            val selector = try {
                ready.get()
                listOf(CameraSelector.DEFAULT_BACK_CAMERA, CameraSelector.DEFAULT_FRONT_CAMERA).firstOrNull { camera.hasCamera(it) }
            } catch (e: Exception) {
                Log.w(TAG, "the camera didn't start", e)
                return@addListener fail(PROBLEM_CAMERA)
            } ?: return@addListener fail(PROBLEM_NO_CAMERA)
            camera.cameraSelector = selector
            camera.bindToLifecycle(this)
            preview.controller = camera
            val info = camera.cameraInfo ?: return@addListener
            light.isVisible = info.hasFlashUnit()
            light.setOnClickListener { camera.enableTorch(camera.torchState.value != TorchState.ON) }
            camera.torchState.observe(this) { showLight(it == TorchState.ON) }
            // Busy (another app has it) passes by itself; disabled or broken doesn't.
            info.cameraState.observe(this) { state ->
                val error = state.error ?: return@observe
                if (error.type == CameraState.ErrorType.CRITICAL) {
                    Log.w(TAG, "camera error ${error.code}", error.cause)
                    fail(PROBLEM_CAMERA)
                }
            }
        }, ContextCompat.getMainExecutor(this))
    }

    /** A frame, on [analysis]: its brightness is all a QR code needs. */
    private fun read(image: ImageProxy) {
        try {
            if (found) return
            val crop = image.cropRect
            val plane = image.planes[0]
            val text = decoder.decode(plane.buffer, plane.rowStride, crop.left, crop.top, crop.width(), crop.height()) ?: return
            found = true
            runOnUiThread { answer(text) }
        } finally {
            image.close()
        }
    }

    private fun answer(text: String) {
        setResult(RESULT_OK, Intent().putExtra(EXTRA_TEXT, text))
        finish()
    }

    private fun fail(problem: String) {
        setResult(RESULT_PROBLEM, Intent().putExtra(EXTRA_PROBLEM, problem))
        finish()
    }

    /** The scanner's words in the language picked in the app ([EXTRA_LANGUAGE]), else the phone's. */
    private fun texts(): Resources {
        val language = intent.getStringExtra(EXTRA_LANGUAGE) ?: return resources
        val config = Configuration(resources.configuration).apply { setLocale(Locale.forLanguageTag(language)) }
        return createConfigurationContext(config).resources
    }

    /** The camera's picture, dimmed around a square to hold the code in; a close button, the light's, and a hint. */
    private fun layout(texts: Resources): View {
        preview = PreviewView(this)
        lightOn = texts.getString(R.string.scan_light_on)
        lightOff = texts.getString(R.string.scan_light_off)
        val close = round(R.drawable.ic_scan_close, texts.getString(R.string.scan_close), GradientDrawable())
        close.setOnClickListener { finish() }
        light = round(R.drawable.ic_scan_light, lightOn, lightShape)
        light.isVisible = false
        showLight(false)
        val hint = TextView(this).apply {
            text = texts.getString(R.string.scan_hint)
            setTextColor(Color.WHITE)
            setTextSize(TypedValue.COMPLEX_UNIT_SP, 16f)
            gravity = Gravity.CENTER
        }
        val controls = FrameLayout(this).apply {
            addView(close, FrameLayout.LayoutParams(dp(48), dp(48), Gravity.TOP or Gravity.START).apply { setMargins(dp(12), dp(12), dp(12), dp(12)) })
            addView(light, FrameLayout.LayoutParams(dp(48), dp(48), Gravity.TOP or Gravity.END).apply { setMargins(dp(12), dp(12), dp(12), dp(12)) })
            addView(hint, FrameLayout.LayoutParams(MATCH_PARENT, WRAP_CONTENT, Gravity.BOTTOM).apply { setMargins(dp(32), 0, dp(32), dp(56)) })
        }
        // Edge to edge: the picture fills the screen, the buttons and the hint keep clear of the bars.
        ViewCompat.setOnApplyWindowInsetsListener(controls) { view, insets ->
            val bars = insets.getInsets(WindowInsetsCompat.Type.systemBars() or WindowInsetsCompat.Type.displayCutout())
            view.setPadding(bars.left, bars.top, bars.right, bars.bottom)
            insets
        }
        return FrameLayout(this).apply {
            addView(preview, MATCH_PARENT, MATCH_PARENT)
            addView(Viewfinder(this@ScanActivity), MATCH_PARENT, MATCH_PARENT)
            addView(controls, MATCH_PARENT, MATCH_PARENT)
        }
    }

    /** A round button over the picture. */
    private fun round(icon: Int, label: String, shape: GradientDrawable) = ImageButton(this).apply {
        setImageResource(icon)
        contentDescription = label
        tooltipText = label
        imageTintList = ColorStateList.valueOf(Color.WHITE)
        shape.shape = GradientDrawable.OVAL
        shape.setColor(SHADE)
        background = RippleDrawable(ColorStateList.valueOf(0x40FFFFFF), shape, null)
    }

    /** The light's button is white while the light is on. */
    private fun showLight(on: Boolean) {
        lightShape.setColor(if (on) Color.WHITE else SHADE)
        light.imageTintList = ColorStateList.valueOf(if (on) Color.BLACK else Color.WHITE)
        light.contentDescription = if (on) lightOff else lightOn
        light.tooltipText = light.contentDescription
    }

    private fun dp(value: Int) = (value * resources.displayMetrics.density).toInt()

    /** Dims the camera's picture around a square, to hold a QR code in. */
    private class Viewfinder(context: Context) : View(context) {
        private val density = resources.displayMetrics.density
        private val dim = Paint().apply { color = SHADE }
        private val edge = Paint(Paint.ANTI_ALIAS_FLAG).apply {
            color = Color.WHITE
            style = Paint.Style.STROKE
            strokeWidth = 3 * density
        }
        private val square = RectF()
        private val around = Path().apply { fillType = Path.FillType.EVEN_ODD }

        init {
            importantForAccessibility = IMPORTANT_FOR_ACCESSIBILITY_NO
        }

        override fun onSizeChanged(w: Int, h: Int, oldw: Int, oldh: Int) {
            val side = minOf(minOf(w, h) * 0.7f, 320 * density)
            square.set((w - side) / 2, (h - side) / 2, (w + side) / 2, (h + side) / 2)
            around.reset()
            around.addRect(0f, 0f, w.toFloat(), h.toFloat(), Path.Direction.CW)
            around.addRoundRect(square, RADIUS * density, RADIUS * density, Path.Direction.CW)
        }

        override fun onDraw(canvas: Canvas) {
            canvas.drawPath(around, dim)
            canvas.drawRoundRect(square, RADIUS * density, RADIUS * density, edge)
        }
    }

    companion object {
        const val EXTRA_LANGUAGE = "language"
        const val EXTRA_TEXT = "text"
        const val EXTRA_PROBLEM = "problem"
        const val RESULT_PROBLEM = Activity.RESULT_FIRST_USER
        const val PROBLEM_PERMISSION = "permission"
        const val PROBLEM_NO_CAMERA = "no_camera"
        const val PROBLEM_CAMERA = "camera"

        private const val TAG = "ScanActivity"
        private const val SHADE = 0x80000000.toInt()
        private const val RADIUS = 24
    }
}
