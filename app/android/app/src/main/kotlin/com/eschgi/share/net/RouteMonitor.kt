package com.eschgi.share.net

import android.annotation.SuppressLint
import android.content.Context
import android.net.ConnectivityManager
import android.net.Network
import android.os.Handler
import android.os.Looper
import com.eschgi.share.data.ServerStore
import java.util.concurrent.CopyOnWriteArraySet
import java.util.concurrent.Executors

/**
 * The one place that decides between the local and the public address, for Dart (route.get
 * and the route events) and for the downloads alike.
 *
 * The local address is probed when something asks and the last answer is older than five
 * minutes, a second after the phone's network changes, and whenever a caller saw it fail.
 * With only a public address nothing is probed.
 */
@SuppressLint("StaticFieldLeak") // holds only the application context
object RouteMonitor {
    private const val FRESH_MS = 5 * 60_000L
    private const val DEBOUNCE_MS = 1_000L

    private lateinit var app: Context
    private val main = Handler(Looper.getMainLooper())
    private val background = Executors.newSingleThreadExecutor { Thread(it, "route").apply { isDaemon = true } }
    private val listeners = CopyOnWriteArraySet<(RouteStatus) -> Unit>()
    private val probing = Any()

    @Volatile private var status = RouteStatus.UNKNOWN

    /** When [status] was probed (System.nanoTime), and for which addresses. */
    @Volatile private var probedAt = 0L
    @Volatile private var probedFor: String? = null

    fun init(context: Context) {
        synchronized(this) {
            if (::app.isInitialized) return
            app = context.applicationContext
        }
        val connectivity = app.getSystemService(ConnectivityManager::class.java)
        connectivity?.registerDefaultNetworkCallback(object : ConnectivityManager.NetworkCallback() {
            override fun onAvailable(network: Network) = networkChanged()
            override fun onLost(network: Network) = networkChanged()
        })
    }

    fun listen(listener: (RouteStatus) -> Unit) = listeners.add(listener)

    fun unlisten(listener: (RouteStatus) -> Unit) = listeners.remove(listener)

    /** The route now, without waiting; starts a probe in the background when it's due. */
    fun current(context: Context): RouteStatus {
        init(context)
        val config = ServerStore(app).config()
        if (config == null || !config.hasLocal) return set(RouteStatus.NO_LOCAL)
        if (due(config.toString())) checkLater()
        return status
    }

    /**
     * Probes the local address and waits for the answer. Callers that arrive during a probe
     * wait for that one instead of starting their own.
     */
    fun check(context: Context): RouteStatus {
        init(context)
        val asked = System.nanoTime()
        synchronized(probing) {
            val config = ServerStore(app).config()
            if (config == null || !config.hasLocal) return set(RouteStatus.NO_LOCAL)
            val key = config.toString()
            if (probedAt > asked && probedFor == key) return status
            set(status.copy(checking = true))
            val result = LocalProbe.probe(config)
            probedFor = key
            probedAt = System.nanoTime()
            return set(result)
        }
    }

    /** Probes in the background, unless another probe got there first. */
    fun checkLater() = background.execute {
        val config = ServerStore(app).config()
        if (config != null && config.hasLocal && due(config.toString())) check(app)
    }

    /** The addresses changed: the last answer was about other ones. */
    fun invalidate() {
        probedAt = 0
        probedFor = null
    }

    private fun due(key: String) =
        probedFor != key || probedAt == 0L || System.nanoTime() - probedAt > FRESH_MS * 1_000_000

    private val recheck = Runnable { checkLater() }

    private fun networkChanged() {
        invalidate()
        main.removeCallbacks(recheck)
        main.postDelayed(recheck, DEBOUNCE_MS)
    }

    private fun set(next: RouteStatus): RouteStatus {
        val changed = next != status
        status = next
        if (changed) listeners.forEach { it(next) }
        return next
    }
}
