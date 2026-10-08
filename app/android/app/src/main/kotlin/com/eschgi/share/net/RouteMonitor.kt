package com.eschgi.share.net

import android.annotation.SuppressLint
import android.content.Context
import android.net.ConnectivityManager
import android.net.Network
import android.os.Handler
import android.os.Looper
import com.eschgi.share.data.SecretStore
import com.eschgi.share.data.ServerConfig
import com.eschgi.share.data.ServerStore
import java.util.concurrent.CopyOnWriteArraySet
import java.util.concurrent.Executors

/**
 * The one place that decides between the local and the public address, for Dart (route.get
 * and the route events) and for the downloads alike.
 *
 * The local address is probed when something asks and the last answer is older than five
 * minutes (half a minute when it didn't answer), a second after the phone's network changes, and
 * whenever a caller saw a request fail, over either address: the public one may not answer at
 * all, e.g. one not set up yet, while the local one answers again.
 * With only a public address nothing is probed, unless that address is plain http (a server
 * only at home): then the probe asks it too, and the phone's key goes there only after it
 * passed on this network ([httpAllowed]).
 */
@SuppressLint("StaticFieldLeak") // holds only the application context
object RouteMonitor {
    private const val FRESH_MS = 5 * 60_000L

    /** A local address that didn't answer is asked again sooner: the server may have been restarting, or the Wi-Fi waking up. */
    private const val RETRY_MS = 30_000L
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

    /**
     * The route now, without waiting; starts a probe in the background when it's due. Until
     * there is an answer for these addresses on this network it says checking, so that a
     * caller can wait for it ([settled]) instead of taking the public address for granted.
     */
    fun current(context: Context): RouteStatus {
        init(context)
        val config = ServerStore(app).config()
        if (config == null || !config.needsProbe) return set(RouteStatus.NO_LOCAL)
        val key = config.toString()
        if (due(key)) checkLater()
        if (probedAt == 0L || probedFor != key) return set(status.copy(checking = true))
        return status
    }

    /** The route for these addresses on this network: [current], or the probe's answer when there is none yet. */
    fun settled(context: Context): RouteStatus {
        val now = current(context)
        return if (now.checking) check(context) else now
    }

    /**
     * Probes the local address and waits for the answer. Callers that arrive during a probe
     * wait for that one instead of starting their own. Unless [quietly], it says checking
     * meanwhile, so that callers wait for the answer; a look again in the background leaves the
     * last answer in use until there is a new one.
     */
    fun check(context: Context, quietly: Boolean = false): RouteStatus {
        init(context)
        val asked = System.nanoTime()
        synchronized(probing) {
            val config = ServerStore(app).config()
            if (config == null || !config.needsProbe) return set(RouteStatus.NO_LOCAL)
            val key = config.toString()
            if (probedAt > asked && probedFor == key) return status
            if (!quietly) set(status.copy(checking = true))
            val result = LocalProbe.probe(config, SecretStore(app).read(SecretStore.DEVICE_TOKEN))
            probedFor = key
            probedAt = System.nanoTime()
            return set(result)
        }
    }

    /**
     * Probes in the background, unless another probe got there first. Quietly: e.g. away from
     * home, where the local address is looked at again every half minute, nothing waits for it.
     */
    fun checkLater() = background.execute {
        val config = ServerStore(app).config()
        if (config != null && config.needsProbe && due(config.toString())) check(app, quietly = true)
    }

    /**
     * The addresses or the network changed: the last answer was about other ones. Until the new
     * ones are probed, nothing local, and nothing to a public address over plain http.
     */
    fun invalidate() {
        probedAt = 0
        probedFor = null
        if (::app.isInitialized && ServerStore(app).config()?.needsProbe == true) set(RouteStatus.UNKNOWN.copy(checking = true))
    }

    /**
     * Whether the phone's key may go to [config]'s local ([local]) or public address over plain
     * http: only after this network's probe found the phone's own server there.
     */
    fun httpAllowed(config: ServerConfig, local: Boolean): Boolean {
        if (probedAt == 0L || probedFor != config.toString()) return false
        return if (local) status.isLocal else status.publicVerified
    }

    private fun due(key: String): Boolean {
        val fresh = if (status.reason == RouteReason.UNREACHABLE) RETRY_MS else FRESH_MS
        return probedFor != key || probedAt == 0L || System.nanoTime() - probedAt > fresh * 1_000_000
    }

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
