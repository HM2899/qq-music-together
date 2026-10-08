/* ==========================================================================
   QQ MUSIC LISTEN TOGETHER - MAIN APP SCRIPT
   ========================================================================== */

// 1. SONG DATABASE WITH DETAILED TIME-SYNCHRONIZED LYRICS
const SONGS = [
    {
        id: "1",
        title: "Midnight Groove",
        artist: "Nightowls",
        album: "Lofi Chill Beats",
        audioUrl: "https://www.soundhelix.com/examples/mp3/SoundHelix-Song-1.mp3",
        coverUrl: "assets/lofi.jpg",
        lyrics: [
            { time: 0, text: "🎵 [智能音效已开启 - 纯音乐前奏] 🎵" },
            { time: 5, text: "夜色渐深，城市里霓虹在闪烁" },
            { time: 10, text: "戴上耳机，听旋律在星空下漂泊" },
            { time: 16, text: "有些故事，在音符里轻轻诉说" },
            { time: 22, text: "一起听，这首歌，感受这瞬间的温热" },
            { time: 29, text: "🎵 [深夜旋律渐入佳境] 🎵" },
            { time: 35, text: "流淌的电音，带走白天的所有疲惫" },
            { time: 41, text: "我们心跳，在虚空信号里交汇" },
            { time: 47, text: "哪怕隔着千山万水，听着同一个音轨" },
            { time: 53, text: "这就是，我们专属的音乐包围" },
            { time: 60, text: "让忧伤随风，让快乐常留心底" },
            { time: 67, text: "听完这首歌，晚安，亲爱的你" },
            { time: 75, text: "🎵 [智能降噪 - 音乐淡出] 🎵" }
        ]
    },
    {
        id: "2",
        title: "Neon Architect",
        artist: "Cybergrid",
        album: "Synthwave Sunset",
        audioUrl: "https://www.soundhelix.com/examples/mp3/SoundHelix-Song-2.mp3",
        coverUrl: "assets/synthwave.jpg",
        lyrics: [
            { time: 0, text: "⚡ [赛博朋克极速前奏] ⚡" },
            { time: 6, text: "穿梭在赛博霓虹，迎着八十年代的晚风" },
            { time: 12, text: "数字太阳缓缓下沉，落入电子地平线中" },
            { time: 18, text: "光流在指尖跃动，音浪将理智放空" },
            { time: 25, text: "这极速的节奏，拉近你我的时空" },
            { time: 31, text: "⚡ [电吉他合成器独奏] ⚡" },
            { time: 38, text: "正版无损音质，享受极致的纯净耳道" },
            { time: 44, text: "房间里闪耀着绿光，这是一起听的讯号" },
            { time: 50, text: "跟随着电子节拍，把所有烦恼都撕掉" },
            { time: 56, text: "在这虚拟城市，我们就是主角" },
            { time: 63, text: "⚡ [高频电音震撼收尾] ⚡" }
        ]
    },
    {
        id: "3",
        title: "Acoustic Sunsets",
        artist: "Willow & Wind",
        album: "Summer Breeze",
        audioUrl: "https://www.soundhelix.com/examples/mp3/SoundHelix-Song-3.mp3",
        coverUrl: "assets/lofi.jpg",
        lyrics: [
            { time: 0, text: "🍃 [清脆民谣木吉他前奏] 🍃" },
            { time: 4, text: "微风吹拂着山谷，稻浪在夕阳下起舞" },
            { time: 9, text: "回家的石子小路，盛开着蓝色的风铃草" },
            { time: 15, text: "你静静靠在窗前，听风中传来的童谣" },
            { time: 21, text: "生活虽然有些忙碌，这首歌陪你笑一笑" },
            { time: 27, text: "🍃 [温柔口琴和弦插入] 🍃" },
            { time: 33, text: "纯真的歌声，是不灭的温暖灯火" },
            { time: 39, text: "正版的高清音质，把自然搬进耳朵" },
            { time: 45, text: "一起听的好友，谢谢你此刻的陪伴" },
            { time: 51, text: "让这份美好，永不散场，永不孤单" },
            { time: 58, text: "🍃 [风铃声伴随吉他渐弱] 🍃" }
        ]
    }
];

// 2. RANDOM USER DATA FOR MOCK AUDIENCE GENERATION
const USER_NAMES = ["节奏大师", "林中听风", "晴天周杰伦", "孤独的黑胶", "流浪音符", "薄荷微醺", "深海鲸鱼", "电子羊梦境"];
const USER_AVATARS = [
    "https://api.dicebear.com/7.x/adventurer/svg?seed=Felix",
    "https://api.dicebear.com/7.x/adventurer/svg?seed=Jack",
    "https://api.dicebear.com/7.x/adventurer/svg?seed=Coco",
    "https://api.dicebear.com/7.x/adventurer/svg?seed=Lily",
    "https://api.dicebear.com/7.x/adventurer/svg?seed=Buddy",
    "https://api.dicebear.com/7.x/adventurer/svg?seed=Ginger"
];

// 3. APPLICATION STATE GLOBALS
let currentSongIndex = 0;
let isPlaying = false;
let isMuted = false;
let userVolume = 0.7;
let soundEffectEnabled = true;

// Listen Together State
let currentRoomId = null;
let userRole = null; // 'host' or 'guest' or null
let myUserId = "user_" + Math.floor(100000 + Math.random() * 900000);
let myUsername = USER_NAMES[Math.floor(Math.random() * USER_NAMES.length)];
let myAvatar = `https://api.dicebear.com/7.x/adventurer/svg?seed=${myUserId}`;
let roomListeners = [];

// Synchronization Sync Channel
const syncChannel = new BroadcastChannel('qq_music_sync_channel');

// Audio Context Elements
let audioCtx = null;
let audioSource = null;
let analyser = null;
let soundEffectNode = null;
let synthIntervalId = null;
let synthAudioContext = null;

// HTML Elements Cache
const audio = document.getElementById('main-audio');
const playPauseBtn = document.getElementById('btn-play-pause');
const playIcon = playPauseBtn.querySelector('.play-icon');
const pauseIcon = playPauseBtn.querySelector('.pause-icon');
const prevBtn = document.getElementById('btn-prev');
const nextBtn = document.getElementById('btn-next');
const shuffleBtn = document.getElementById('btn-shuffle');
const loopBtn = document.getElementById('btn-loop');
const favoriteBtn = document.getElementById('btn-favorite');

const progressBarWrapper = document.getElementById('progress-bar-wrapper');
const progressBarFill = document.getElementById('progress-bar-fill');
const progressHandle = document.getElementById('progress-handle');
const timeElapsed = document.getElementById('time-elapsed');
const timeTotal = document.getElementById('time-total');

const volumeMuteToggle = document.getElementById('btn-mute-toggle');
const volumeIcon = document.getElementById('volume-icon');
const volumeSliderWrapper = document.getElementById('volume-slider-wrapper');
const volumeBarFill = document.getElementById('volume-bar-fill');
const volumeHandle = document.getElementById('volume-handle');

const vinylRecord = document.getElementById('vinyl-record');
const tonearm = document.getElementById('tonearm');
const vinylCover = document.getElementById('vinyl-cover');
const playerSongTitle = document.getElementById('player-song-title');
const playerSongArtist = document.getElementById('player-song-artist');
const playerSongAlbum = document.getElementById('player-song-album');
const barCover = document.getElementById('bar-cover');
const barTitle = document.getElementById('bar-title');
const barArtist = document.getElementById('bar-artist');
const lyricsScroll = document.getElementById('lyrics-scroll');
const lyricsWrapper = document.getElementById('lyrics-wrapper');

const togetherPanel = document.getElementById('together-panel');
const roomSetupView = document.getElementById('room-setup-view');
const roomActiveView = document.getElementById('room-active-view');
const roomStatusBadge = document.getElementById('room-status-badge');
const activeRoomCode = document.getElementById('active-room-code');
const listenersList = document.getElementById('listeners-list');
const listenerCountEl = document.getElementById('listener-count');
const chatMessagesBox = document.getElementById('chat-messages-box');
const chatTextInput = document.getElementById('chat-text-input');
const chatSendForm = document.getElementById('chat-send-form');

const btnCreateRoom = document.getElementById('btn-create-room');
const btnJoinRoom = document.getElementById('btn-join-room');
const btnLeaveRoom = document.getElementById('btn-leave-room');
const inputRoomCode = document.getElementById('input-room-code');
const btnCopyCode = document.getElementById('btn-copy-code');
const btnBarTogether = document.getElementById('btn-bar-together');
const togetherBadge = document.getElementById('together-badge-indicator');

const shareModal = document.getElementById('share-modal');
const btnCloseShareModal = document.getElementById('btn-close-share-modal');
const shareModalCode = document.getElementById('share-modal-code');
const shareModalLink = document.getElementById('share-modal-link');
const btnCopyShareLink = document.getElementById('btn-copy-share-link');

const canvas = document.getElementById('visualizer-canvas');
const canvasCtx = canvas.getContext('2d');
const soundEffectToggle = document.getElementById('sound-effect-toggle');
const togglePanelSidebar = document.getElementById('toggle-panel-sidebar');
const navTogetherBtn = document.getElementById('nav-together-btn');
const navRecommend = document.getElementById('nav-recommend');
const navRadio = document.getElementById('nav-radio');
const immersiveBg = document.getElementById('immersive-bg');

// ==========================================================================
// 4. MAIN MUSIC PLAYER ENGINE
// ==========================================================================

function initPlayer() {
    loadSong(currentSongIndex);
    setupEventListeners();
    initVisualizerCanvas();
    checkUrlForRoomCode();
}

function loadSong(index) {
    const song = SONGS[index];
    
    // Load audio element source
    audio.src = song.audioUrl;
    audio.volume = isMuted ? 0 : userVolume;
    
    // Update core UI metadata
    playerSongTitle.textContent = song.title;
    playerSongArtist.textContent = song.artist;
    playerSongAlbum.textContent = song.album;
    barTitle.textContent = song.title;
    barArtist.textContent = song.artist;
    
    vinylCover.src = song.coverUrl;
    barCover.src = song.coverUrl;
    immersiveBg.style.backgroundImage = `url('${song.coverUrl}')`;
    
    // Generate and render lyrics
    renderLyrics(song.lyrics);
    
    // Reset times
    timeElapsed.textContent = "00:00";
    timeTotal.textContent = "00:00";
    progressBarFill.style.width = "0%";
    progressHandle.style.left = "0%";

    // Auto play if state was previously playing
    if (isPlaying) {
        audio.play().catch(handleAudioLoadError);
    }
}

// Fallback Synthesizer beats in case audio url fails (CORS or network issues)
function handleAudioLoadError(err) {
    console.warn("Audio URL load failed or blocked by CORS. Activating Local Audio Synthesizer fallback...", err);
    
    // Add system notification in the chat if in a room
    if (currentRoomId) {
        addSystemMessage("提示：远程音频流加载受限，已激活本地无损数字合成引擎播放。");
    }
    
    // If playing, we run our mock synth beat
    if (isPlaying) {
        startSynthesizerBeats();
    }
}

function togglePlay() {
    // Guest cannot play/pause on their own when in a synced room
    if (currentRoomId && userRole === 'guest') {
        showToast("只有房主可以进行播控操作哦");
        return;
    }

    if (isPlaying) {
        pauseAudio();
    } else {
        playAudio();
    }

    // Synchronize play state if host
    if (currentRoomId && userRole === 'host') {
        broadcastStateChange('play_pause', isPlaying, audio.currentTime);
    }
}

function playAudio() {
    isPlaying = true;
    
    // Initialize Web Audio context on user gesture
    initAudioContext();
    
    // Start playback
    audio.play()
        .then(() => {
            stopSynthesizerBeats(); // stop synth if standard audio works
        })
        .catch(handleAudioLoadError);
        
    playIcon.classList.add('hidden');
    pauseIcon.classList.remove('hidden');
    vinylRecord.classList.add('playing');
    tonearm.classList.add('active');
}

function pauseAudio() {
    isPlaying = false;
    audio.pause();
    stopSynthesizerBeats();
    
    playIcon.classList.remove('hidden');
    pauseIcon.classList.add('hidden');
    vinylRecord.classList.remove('playing');
    tonearm.classList.remove('active');
}

function nextSong() {
    if (currentRoomId && userRole === 'guest') {
        showToast("只有房主拥有切歌控制权");
        return;
    }

    currentSongIndex = (currentSongIndex + 1) % SONGS.length;
    loadSong(currentSongIndex);
    
    if (currentRoomId && userRole === 'host') {
        broadcastTrackChange();
    }
}

function prevSong() {
    if (currentRoomId && userRole === 'guest') {
        showToast("只有房主拥有切歌控制权");
        return;
    }

    currentSongIndex = (currentSongIndex - 1 + SONGS.length) % SONGS.length;
    loadSong(currentSongIndex);

    if (currentRoomId && userRole === 'host') {
        broadcastTrackChange();
    }
}

// ==========================================================================
// 5. WEB AUDIO API SYNTHESIZER FALLBACK
// ==========================================================================

function startSynthesizerBeats() {
    if (synthIntervalId) return;

    if (!synthAudioContext) {
        synthAudioContext = new (window.AudioContext || window.webkitAudioContext)();
    }
    
    // Connect analyzer to synthesizers if needed
    if (synthAudioContext && analyser) {
        // If we are synthesis-only, we can wire oscillators to analyser
    }

    let beatCount = 0;
    
    // Synthesize simple aesthetic beats (Lofi/Synthwave)
    synthIntervalId = setInterval(() => {
        if (!isPlaying) return;
        
        try {
            const time = synthAudioContext.currentTime;
            
            // Kick drum synth (Low frequency sweep)
            if (beatCount % 2 === 0) {
                const kick = synthAudioContext.createOscillator();
                const kickGain = synthAudioContext.createGain();
                kick.connect(kickGain);
                
                // Connect to destination AND analyser to show wave bars
                kickGain.connect(synthAudioContext.destination);
                
                kick.frequency.setValueAtTime(150, time);
                kick.frequency.exponentialRampToValueAtTime(0.01, time + 0.3);
                
                kickGain.gain.setValueAtTime(0.6, time);
                kickGain.gain.exponentialRampToValueAtTime(0.01, time + 0.3);
                
                kick.start(time);
                kick.stop(time + 0.35);
            }

            // Synth chord harmony (Aesthetic backing track)
            if (beatCount % 8 === 0) {
                const chordNotes = [220, 277.18, 329.63, 440]; // A major lofi chord
                chordNotes.forEach(freq => {
                    const osc = synthAudioContext.createOscillator();
                    const oscGain = synthAudioContext.createGain();
                    osc.type = 'triangle';
                    osc.connect(oscGain);
                    oscGain.connect(synthAudioContext.destination);
                    
                    osc.frequency.setValueAtTime(freq, time);
                    
                    oscGain.gain.setValueAtTime(0, time);
                    oscGain.gain.linearRampToValueAtTime(0.12, time + 0.2);
                    oscGain.gain.exponentialRampToValueAtTime(0.01, time + 2.0);
                    
                    osc.start(time);
                    osc.stop(time + 2.1);
                });
            }
            
            beatCount++;
            
            // Increment fallback fake time for guest sync compatibility
            if (audio.paused) {
                audio.currentTime = (audio.currentTime + 0.5) % (audio.duration || 180);
                updatePlaybackProgress();
            }
            
        } catch (e) {
            console.error("Synthesizer error", e);
        }
    }, 500);
}

function stopSynthesizerBeats() {
    if (synthIntervalId) {
        clearInterval(synthIntervalId);
        synthIntervalId = null;
    }
}

// ==========================================================================
// 6. AUDIO VISUALIZER & DYNAMIC GRAPHICS
// ==========================================================================

function initAudioContext() {
    if (audioCtx) return;

    try {
        const AudioContextClass = window.AudioContext || window.webkitAudioContext;
        audioCtx = new AudioContextClass();
        
        // Analyzer Node
        analyser = audioCtx.createAnalyser();
        analyser.fftSize = 64; // Small size for responsive minimalist bar wave
        
        // Equalizer / High frequency boost filter (Smart Sound Effect toggle)
        soundEffectNode = audioCtx.createBiquadFilter();
        soundEffectNode.type = "highshelf";
        soundEffectNode.frequency.value = 8000;
        soundEffectNode.gain.value = soundEffectEnabled ? 6 : 0;

        // Source node connection
        audioSource = audioCtx.createMediaElementSource(audio);
        audioSource.connect(soundEffectNode);
        soundEffectNode.connect(analyser);
        analyser.connect(audioCtx.destination);
        
        renderVisualizer();
    } catch(e) {
        console.warn("Web Audio API context could not build (unsupported or blocked).", e);
    }
}

function initVisualizerCanvas() {
    canvas.width = canvas.parentElement.clientWidth;
    canvas.height = 60;
    
    // Draw initial idle lines
    drawIdleVisualizer();
    
    window.addEventListener('resize', () => {
        canvas.width = canvas.parentElement.clientWidth;
        drawIdleVisualizer();
    });
}

function drawIdleVisualizer() {
    canvasCtx.clearRect(0, 0, canvas.width, canvas.height);
    const barWidth = 4;
    const gap = 6;
    const numBars = Math.floor(canvas.width / (barWidth + gap));
    
    canvasCtx.fillStyle = 'rgba(49, 194, 124, 0.2)';
    for(let i=0; i < numBars; i++) {
        const x = i * (barWidth + gap);
        // Draw standard subtle tiny bars
        const h = 4 + Math.sin(i * 0.15) * 2;
        const y = canvas.height - h;
        canvasCtx.fillRect(x, y, barWidth, h);
    }
}

function renderVisualizer() {
    if (!analyser) return;
    
    requestAnimationFrame(renderVisualizer);
    
    const bufferLength = analyser.frequencyBinCount;
    const dataArray = new Uint8Array(bufferLength);
    analyser.getByteFrequencyData(dataArray);
    
    canvasCtx.clearRect(0, 0, canvas.width, canvas.height);
    
    const barWidth = 6;
    const gap = 8;
    const numBars = Math.floor(canvas.width / (barWidth + gap));
    
    for (let i = 0; i < numBars; i++) {
        const dataIdx = Math.floor((i / numBars) * bufferLength);
        let value = isPlaying ? dataArray[dataIdx] : 0;
        
        // Amplify value if audio stream is silent/fallback but playing
        if (isPlaying && value === 0) {
            value = 20 + Math.sin(Date.now() * 0.01 + i) * 15; // Simulated vibe
        }
        
        // Calculate height
        const percent = value / 255;
        const h = Math.max(4, percent * canvas.height * 0.95);
        const y = canvas.height - h;
        const x = i * (barWidth + gap);
        
        // Create custom gradient for bars (QQ music green neon look)
        const barGrad = canvasCtx.createLinearGradient(0, y, 0, canvas.height);
        barGrad.addColorStop(0, '#41e292');
        barGrad.addColorStop(1, '#31c27c');
        
        canvasCtx.fillStyle = barGrad;
        // Rounded top bars
        canvasCtx.beginPath();
        canvasCtx.roundRect(x, y, barWidth, h, 3);
        canvasCtx.fill();
        
        // Glowing shadow overlay
        if (isPlaying) {
            canvasCtx.shadowColor = 'rgba(49, 194, 124, 0.35)';
            canvasCtx.shadowBlur = 8;
        } else {
            canvasCtx.shadowBlur = 0;
        }
    }
}

function toggleSoundEffect() {
    soundEffectEnabled = !soundEffectEnabled;
    if (soundEffectEnabled) {
        soundEffectToggle.classList.add('action-btn');
        soundEffectToggle.classList.remove('btn-secondary');
        soundEffectToggle.innerHTML = `<i data-lucide="sparkles"></i> <span>智能音效: 已开启</span>`;
        if (soundEffectNode) soundEffectNode.gain.value = 6; // Boost highs
    } else {
        soundEffectToggle.classList.remove('action-btn');
        soundEffectToggle.classList.add('btn-secondary');
        soundEffectToggle.style.backgroundColor = "rgba(255,255,255,0.05)";
        soundEffectToggle.style.border = "1px solid var(--border-color)";
        soundEffectToggle.style.color = "var(--text-muted)";
        soundEffectToggle.innerHTML = `<i data-lucide="sparkles"></i> <span>智能音效: 已关闭</span>`;
        if (soundEffectNode) soundEffectNode.gain.value = 0;
    }
    lucide.createIcons();
    showToast(`智能音效已${soundEffectEnabled ? '开启' : '关闭'}`);
}

// ==========================================================================
// 7. TIME PROGRESSION & LYRICS SYNCING
// ==========================================================================

function updatePlaybackProgress() {
    const curTime = audio.currentTime;
    const dur = audio.duration || 180; // Fallback 3 mins
    
    // Elapsed and total formatting
    timeElapsed.textContent = formatTime(curTime);
    timeTotal.textContent = formatTime(dur);
    
    // Percentage
    const pct = (curTime / dur) * 100;
    progressBarFill.style.width = `${pct}%`;
    progressHandle.style.left = `${pct}%`;
    
    // Sync current lyric line
    syncLyrics(curTime);
}

function formatTime(secs) {
    const m = Math.floor(secs / 60).toString().padStart(2, '0');
    const s = Math.floor(secs % 60).toString().padStart(2, '0');
    return `${m}:${s}`;
}

function renderLyrics(lyricArray) {
    lyricsWrapper.innerHTML = "";
    
    if (!lyricArray || lyricArray.length === 0) {
        lyricsWrapper.innerHTML = `<p class="lyric-line placeholder">纯音乐，无歌词</p>`;
        return;
    }
    
    lyricArray.forEach((line, index) => {
        const p = document.createElement('p');
        p.className = 'lyric-line';
        p.dataset.time = line.time;
        p.dataset.index = index;
        p.textContent = line.text;
        
        // Clicking lyrics line allows host to jump directly to section
        p.addEventListener('click', () => {
            if (currentRoomId && userRole === 'guest') {
                showToast("只有房主可以调整播放进度");
                return;
            }
            audio.currentTime = line.time;
            updatePlaybackProgress();
            if (currentRoomId && userRole === 'host') {
                broadcastStateChange('seek', isPlaying, audio.currentTime);
            }
        });
        
        lyricsWrapper.appendChild(p);
    });
}

function syncLyrics(time) {
    const lines = lyricsWrapper.querySelectorAll('.lyric-line');
    if (lines.length === 0) return;
    
    let activeLine = null;
    
    // Traverse lines to see which matches current playhead time
    for (let i = 0; i < lines.length; i++) {
        const lineTime = parseFloat(lines[i].dataset.time);
        const nextLineTime = lines[i+1] ? parseFloat(lines[i+1].dataset.time) : Infinity;
        
        if (time >= lineTime && time < nextLineTime) {
            activeLine = lines[i];
            break;
        }
    }
    
    if (activeLine && !activeLine.classList.contains('active')) {
        // Clear old actives
        lines.forEach(l => l.classList.remove('active'));
        
        // Highlight active line
        activeLine.classList.add('active');
        
        // Scroll to center active lyric line
        const containerHeight = lyricsScroll.clientHeight;
        const lineTop = activeLine.offsetTop;
        const lineHeight = activeLine.clientHeight;
        const scrollTarget = lineTop - (containerHeight / 2) + (lineHeight / 2);
        
        lyricsScroll.scrollTo({
            top: scrollTarget,
            behavior: 'smooth'
        });
    }
}

// Scrubber seek logic
function handleTimelineScrub(e) {
    if (currentRoomId && userRole === 'guest') {
        showToast("只有房主可以调整进度轴");
        return;
    }
    
    const rect = progressBarWrapper.getBoundingClientRect();
    const clickX = e.clientX - rect.left;
    const pct = Math.max(0, Math.min(1, clickX / rect.width));
    
    const dur = audio.duration || 180;
    audio.currentTime = pct * dur;
    
    updatePlaybackProgress();
    
    if (currentRoomId && userRole === 'host') {
        broadcastStateChange('seek', isPlaying, audio.currentTime);
    }
}

// Volume bar adjustment
function handleVolumeAdjust(e) {
    const rect = volumeSliderWrapper.getBoundingClientRect();
    const clickX = e.clientX - rect.left;
    const pct = Math.max(0, Math.min(1, clickX / rect.width));
    
    userVolume = pct;
    audio.volume = userVolume;
    isMuted = false;
    
    volumeBarFill.style.width = `${pct * 100}%`;
    volumeHandle.style.left = `${pct * 100}%`;
    
    updateVolumeIcon();
}

function toggleMute() {
    isMuted = !isMuted;
    if (isMuted) {
        audio.volume = 0;
        volumeBarFill.style.width = `0%`;
        volumeHandle.style.left = `0%`;
        volumeIcon.setAttribute('data-lucide', 'volume-x');
    } else {
        audio.volume = userVolume;
        volumeBarFill.style.width = `${userVolume * 100}%`;
        volumeHandle.style.left = `${userVolume * 100}%`;
        updateVolumeIcon();
    }
    lucide.createIcons();
}

function updateVolumeIcon() {
    if (userVolume === 0) {
        volumeIcon.setAttribute('data-lucide', 'volume-x');
    } else if (userVolume < 0.4) {
        volumeIcon.setAttribute('data-lucide', 'volume-1');
    } else {
        volumeIcon.setAttribute('data-lucide', 'volume-2');
    }
    lucide.createIcons();
}

// ==========================================================================
// 8. LISTEN TOGETHER SYNCHRONIZATION SYSTEM
// ==========================================================================

// Create Room logic
function createRoom() {
    currentRoomId = Math.floor(100000 + Math.random() * 900000).toString();
    userRole = 'host';
    
    // Add user as listener
    roomListeners = [{
        userId: myUserId,
        username: myUsername + " (你)",
        avatar: myAvatar,
        role: 'host',
        isVip: true
    }];
    
    // Update interface
    enterRoomUIState();
    addSystemMessage(`一起听房间创建成功！房间号是 ${currentRoomId}。`);
    
    // Auto-generate a dummy friend in 3 seconds to make it feel organic
    setTimeout(() => {
        if (currentRoomId) {
            simulateFriendJoining();
        }
    }, 4000);
}

// Join Room logic
function joinRoom(roomCode) {
    if (!roomCode || roomCode.trim().length !== 6) {
        showToast("请输入有效的 6 位数字房间码");
        return;
    }
    
    currentRoomId = roomCode.trim();
    userRole = 'guest';
    
    // Clear chat
    chatMessagesBox.innerHTML = "";
    
    enterRoomUIState();
    addSystemMessage(`正在尝试加入房间 ${currentRoomId}...`);
    
    // Broadcast join request to other tabs
    sendBroadcastMessage('SYNC_REQUEST', {
        userId: myUserId,
        username: myUsername,
        avatar: myAvatar,
        roomId: currentRoomId
    });
    
    // Add myself locally to start with
    roomListeners = [{
        userId: myUserId,
        username: myUsername + " (你)",
        avatar: myAvatar,
        role: 'guest',
        isVip: true
    }];
    renderListeners();
    
    // Set a timer: if no host responds in 2.5s, create host context mock
    setTimeout(() => {
        if (userRole === 'guest' && roomListeners.length === 1) {
            simulateHostResponding();
        }
    }, 2500);
}

function leaveRoom() {
    if (!currentRoomId) return;
    
    // Broadcast leave
    sendBroadcastMessage('ROOM_LEFT', {
        userId: myUserId,
        roomId: currentRoomId
    });
    
    currentRoomId = null;
    userRole = null;
    roomListeners = [];
    
    exitRoomUIState();
    showToast("已退出一起听房间");
}

function checkUrlForRoomCode() {
    const params = new URLSearchParams(window.location.search);
    const roomCode = params.get('room');
    if (roomCode) {
        joinRoom(roomCode);
    }
}

// Send Broadcast channels helper
function sendBroadcastMessage(type, payload) {
    syncChannel.postMessage({ type, payload });
}

// Handle incoming Multi-Tab Sync Events
syncChannel.onmessage = function (event) {
    const { type, payload } = event.data;
    
    if (!currentRoomId) return; // Ignore if not in any room
    
    // If incoming message belongs to a different room, discard
    if (payload.roomId && payload.roomId !== currentRoomId) return;

    switch (type) {
        case 'SYNC_REQUEST':
            // If I am the HOST, I need to send state snapshot to the new guest
            if (userRole === 'host') {
                // Add new guest to listener list
                if (!roomListeners.some(u => u.userId === payload.userId)) {
                    roomListeners.push({
                        userId: payload.userId,
                        username: payload.username,
                        avatar: payload.avatar,
                        role: 'guest',
                        isVip: true
                    });
                    renderListeners();
                }
                
                // Broadcast updated user lists and current player state
                sendBroadcastMessage('SYNC_STATE', {
                    roomId: currentRoomId,
                    listeners: roomListeners,
                    songIndex: currentSongIndex,
                    isPlaying: isPlaying,
                    currentTime: audio.currentTime
                });
                
                addSystemMessage(`QQ音乐正版用户 “${payload.username}” 加入了房间。`);
            }
            break;
            
        case 'SYNC_STATE':
            // If I am the GUEST, adjust my playback to align with Host state
            if (userRole === 'guest') {
                roomListeners = payload.listeners.map(user => {
                    if (user.userId === myUserId) {
                        return { ...user, username: myUsername + " (你)" };
                    }
                    return user;
                });
                renderListeners();
                
                // Align song
                if (currentSongIndex !== payload.songIndex) {
                    currentSongIndex = payload.songIndex;
                    loadSong(currentSongIndex);
                }
                
                // Align play time with dynamic buffer compensation
                const timeDiff = Math.abs(audio.currentTime - payload.currentTime);
                if (timeDiff > 1.5) {
                    audio.currentTime = payload.currentTime;
                }
                
                // Align play/pause state
                if (payload.isPlaying) {
                    playAudio();
                } else {
                    pauseAudio();
                }
                
                addSystemMessage("成功连接房间！已同步房主的音乐进度。");
            }
            break;
            
        case 'PLAY_STATE_CHANGE':
            if (userRole === 'guest') {
                const { action, currentTime } = payload;
                const timeDiff = Math.abs(audio.currentTime - currentTime);
                
                if (timeDiff > 1.2) {
                    audio.currentTime = currentTime;
                }
                
                if (action === 'play') {
                    playAudio();
                    addSystemMessage("房主 开始了 播放");
                } else if (action === 'pause') {
                    pauseAudio();
                    addSystemMessage("房主 暂停了 播放");
                } else if (action === 'seek') {
                    addSystemMessage(`房主 调整进度至 ${formatTime(currentTime)}`);
                }
            }
            break;
            
        case 'TRACK_CHANGE':
            if (userRole === 'guest') {
                currentSongIndex = payload.songIndex;
                loadSong(currentSongIndex);
                addSystemMessage(`房主 切歌为 《${SONGS[currentSongIndex].title}》`);
            }
            break;
            
        case 'CHAT_MESSAGE':
            // Append incoming message from other user
            if (payload.userId !== myUserId) {
                appendChatMessage(payload.username, payload.text, false);
            }
            break;
            
        case 'REACTION':
            // Fire animated emoji reaction on current page
            triggerFloatingEmoji(payload.emoji);
            // Print brief notice in chat log
            if (payload.userId !== myUserId) {
                addSystemMessage(`“${payload.username}” 送出了互动表情 ${payload.emoji}`);
            }
            break;
            
        case 'ROOM_LEFT':
            // Remove listener from list
            roomListeners = roomListeners.filter(u => u.userId !== payload.userId);
            renderListeners();
            
            // If the host left and I am a guest, notify and demote room state
            const hostUser = roomListeners.find(u => u.role === 'host');
            if (!hostUser && userRole === 'guest') {
                addSystemMessage("房主已离开，房间已自动解散。");
                setTimeout(() => {
                    leaveRoom();
                }, 3000);
            } else {
                const leavingUser = roomListeners.find(u => u.userId === payload.userId);
                addSystemMessage(`听众 “${leavingUser ? leavingUser.username : '好友'}” 离开了房间。`);
            }
            break;
    }
};

function broadcastStateChange(action, isPlaying, time) {
    sendBroadcastMessage('PLAY_STATE_CHANGE', {
        roomId: currentRoomId,
        action: action === 'seek' ? 'seek' : (isPlaying ? 'play' : 'pause'),
        currentTime: time
    });
}

function broadcastTrackChange() {
    sendBroadcastMessage('TRACK_CHANGE', {
        roomId: currentRoomId,
        songIndex: currentSongIndex
    });
}

function sendChatMessage(text) {
    if (!text || text.trim() === "") return;
    
    appendChatMessage("你", text, true);
    
    if (currentRoomId) {
        sendBroadcastMessage('CHAT_MESSAGE', {
            roomId: currentRoomId,
            userId: myUserId,
            username: myUsername,
            text: text
        });
    }
    
    // Simulate chat mock reaction from bot if alone
    if (currentRoomId && roomListeners.length > 1) {
        // If there's a bot (user with Felix or Jack avatar)
        const hasBot = roomListeners.some(u => u.userId.startsWith('bot_'));
        if (hasBot) {
            simulateBotChatResponse(text);
        }
    }
}

function sendReaction(emoji) {
    triggerFloatingEmoji(emoji);
    
    if (currentRoomId) {
        sendBroadcastMessage('REACTION', {
            roomId: currentRoomId,
            userId: myUserId,
            username: myUsername,
            emoji: emoji
        });
    }
}

// ==========================================================================
// 9. ORGANIC ROOM SIMULATION (FOR SINGLE-TAB TESTING)
// ==========================================================================

function simulateFriendJoining() {
    const mockId = "bot_" + Math.floor(Math.random() * 9000);
    const mockName = USER_NAMES[Math.floor(Math.random() * USER_NAMES.length)];
    const mockAvatar = USER_AVATARS[Math.floor(Math.random() * USER_AVATARS.length)];
    
    const botUser = {
        userId: mockId,
        username: mockName,
        avatar: mockAvatar,
        role: 'guest',
        isVip: true
    };
    
    roomListeners.push(botUser);
    renderListeners();
    addSystemMessage(`QQ音乐正版用户 “${mockName}” 扫码加入了你的房间。`);
    
    // Trigger a heart emoji from the mock bot as warm greeting
    setTimeout(() => {
        if (currentRoomId) {
            sendReaction("❤️");
            addSystemMessage(`“${mockName}” 送出了互动表情 ❤️`);
        }
    }, 1800);
}

function simulateHostResponding() {
    // If no real tab responses, simulate an interactive Host tab so it is testable offline
    const mockId = "bot_host_" + Math.floor(Math.random() * 9000);
    const mockName = "云端房主-" + USER_NAMES[Math.floor(Math.random() * USER_NAMES.length)];
    const mockAvatar = USER_AVATARS[0];
    
    const hostUser = {
        userId: mockId,
        username: mockName,
        avatar: mockAvatar,
        role: 'host',
        isVip: true
    };
    
    roomListeners.unshift(hostUser);
    renderListeners();
    
    // Update lyrics and play active track
    currentSongIndex = 0; // Default first
    loadSong(currentSongIndex);
    playAudio();
    
    addSystemMessage(`房主 “${mockName}” 同意了你的监听请求。`);
    addSystemMessage("已成功加载同步的歌曲音频...");
}

function simulateBotChatResponse(userMsg) {
    // Simple mock responses
    let reply = "这首歌真的太好听了！🎵";
    if (userMsg.includes("歌词") || userMsg.includes("词")) {
        reply = "是啊，这句歌词直接戳中我了。✨";
    } else if (userMsg.includes("切歌") || userMsg.includes("换一首")) {
        reply = "期待房主切一首燃一点的！";
    } else if (userMsg.includes("绿钻") || userMsg.includes("会员")) {
        reply = "正版音效确实不一样，声浪很细腻！👍";
    } else if (userMsg.includes("你好") || userMsg.includes("嗨")) {
        reply = "嗨！很高兴能和你一起听这首歌，音乐真的很有魔力！";
    }
    
    const bot = roomListeners.find(u => u.userId.startsWith('bot_'));
    const botName = bot ? bot.username : "好友";
    
    setTimeout(() => {
        if (currentRoomId) {
            appendChatMessage(botName, reply, false);
        }
    }, 1500 + Math.random() * 1500);
}

// ==========================================================================
// 10. INTERFACE RENDERERS & EVENT HANDLERS
// ==========================================================================

function renderListeners() {
    listenersList.innerHTML = "";
    listenerCountEl.textContent = roomListeners.length;
    
    roomListeners.forEach(user => {
        const div = document.createElement('div');
        div.className = `listener-item ${user.role === 'host' ? 'host' : ''}`;
        
        div.innerHTML = `
            <img src="${user.avatar}" alt="Avatar" class="avatar">
            <div class="listener-details">
                <div class="listener-name-row">
                    <span class="listener-name">${user.username}</span>
                    ${user.role === 'host' ? '<span class="role-badge">房主</span>' : ''}
                </div>
                <span class="listener-status-desc">${user.role === 'host' ? '正在主导播控' : '正在同步监听中'}</span>
            </div>
            <div class="vip-status-check">
                <i data-lucide="shield" class="vip-icon" title="正版QQ绿钻用户"></i>
            </div>
        `;
        listenersList.appendChild(div);
    });
    
    lucide.createIcons();
}

function appendChatMessage(sender, text, isMine) {
    const div = document.createElement('div');
    div.className = `chat-bubble ${isMine ? 'mine' : 'other'}`;
    
    div.innerHTML = `
        <span class="chat-sender-name">${sender}</span>
        <span>${escapeHtml(text)}</span>
    `;
    
    chatMessagesBox.appendChild(div);
    chatMessagesBox.scrollTop = chatMessagesBox.scrollHeight;
}

function addSystemMessage(text) {
    const div = document.createElement('div');
    div.className = 'chat-system-msg';
    div.textContent = text;
    chatMessagesBox.appendChild(div);
    chatMessagesBox.scrollTop = chatMessagesBox.scrollHeight;
}

function escapeHtml(text) {
    const map = {
        '&': '&amp;',
        '<': '&lt;',
        '>': '&gt;',
        '"': '&quot;',
        "'": '&#039;'
    };
    return text.replace(/[&<>"']/g, m => map[m]);
}

function enterRoomUIState() {
    roomSetupView.classList.add('hidden');
    roomActiveView.classList.remove('hidden');
    
    roomStatusBadge.textContent = "已建房";
    roomStatusBadge.classList.remove('inactive');
    roomStatusBadge.classList.add('active');
    
    activeRoomCode.textContent = currentRoomId;
    togetherBadge.classList.remove('hidden');
    togetherBadge.textContent = "●";
    
    // Enable invite trigger pulse
    navTogetherBtn.classList.add('active-room-pulse');
}

function exitRoomUIState() {
    roomSetupView.classList.remove('hidden');
    roomActiveView.classList.add('hidden');
    
    roomStatusBadge.textContent = "未建房";
    roomStatusBadge.classList.add('inactive');
    roomStatusBadge.classList.remove('active');
    
    togetherBadge.classList.add('hidden');
    navTogetherBtn.classList.remove('active-room-pulse');
}

function showShareModal() {
    if (!currentRoomId) {
        showToast("请先创建或加入一个一起听房间");
        return;
    }
    
    shareModalCode.textContent = currentRoomId;
    
    // Create query url
    const currentLoc = window.location.href.split('?')[0];
    const inviteUrl = `${currentLoc}?room=${currentRoomId}`;
    shareModalLink.value = inviteUrl;
    
    shareModal.classList.remove('hidden');
}

function closeShareModal() {
    shareModal.classList.add('hidden');
}

function triggerFloatingEmoji(emoji) {
    const container = document.getElementById('floating-reactions-overlay');
    const el = document.createElement('div');
    el.className = 'floating-emoji';
    el.textContent = emoji;
    
    // Dynamic starting points from bottom right corner (soundboard region)
    const rightPanelWidth = 380;
    const startX = window.innerWidth - (rightPanelWidth / 2) - 30 + (Math.random() * 80 - 40);
    
    el.style.left = `${startX}px`;
    
    // Dynamic horizontal movements
    const driftMid = (Math.random() * 120 - 60) + "px";
    const driftEnd = (Math.random() * 240 - 120) + "px";
    el.style.setProperty('--drift-x-mid', driftMid);
    el.style.setProperty('--drift-x-end', driftEnd);
    
    container.appendChild(el);
    
    // Remove element after transition ends
    setTimeout(() => {
        el.remove();
    }, 3000);
}

function showToast(message) {
    // Elegant temporary bubble
    const toast = document.createElement('div');
    toast.style.position = 'fixed';
    toast.style.bottom = '110px';
    toast.style.left = '50%';
    toast.style.transform = 'translateX(-50%)';
    toast.style.background = 'rgba(15, 23, 42, 0.9)';
    toast.style.border = '1px solid var(--border-active)';
    toast.style.color = '#fff';
    toast.style.padding = '10px 20px';
    toast.style.borderRadius = '20px';
    toast.style.fontSize = '13px';
    toast.style.zIndex = '9999';
    toast.style.boxShadow = '0 10px 25px rgba(0,0,0,0.5), 0 0 10px var(--primary-glow)';
    toast.style.opacity = '0';
    toast.style.transition = 'opacity 0.3s ease';
    toast.textContent = message;
    
    document.body.appendChild(toast);
    
    setTimeout(() => toast.style.opacity = '1', 50);
    setTimeout(() => {
        toast.style.opacity = '0';
        setTimeout(() => toast.remove(), 300);
    }, 2800);
}

// ==========================================================================
// 11. EVENT LISTENER BINDINGS
// ==========================================================================

function setupEventListeners() {
    // Play control clicks
    playPauseBtn.addEventListener('click', togglePlay);
    nextBtn.addEventListener('click', nextSong);
    prevBtn.addEventListener('click', prevSong);
    
    audio.addEventListener('timeupdate', updatePlaybackProgress);
    audio.addEventListener('ended', nextSong);
    
    // Timeline clicks
    progressBarWrapper.addEventListener('click', handleTimelineScrub);
    
    // Volume clicks
    volumeMuteToggle.addEventListener('click', toggleMute);
    volumeSliderWrapper.addEventListener('click', handleVolumeAdjust);
    
    // Soundboard
    soundEffectToggle.addEventListener('click', toggleSoundEffect);
    
    // Listen Together Control Clicks
    btnCreateRoom.addEventListener('click', createRoom);
    btnJoinRoom.addEventListener('click', () => {
        joinRoom(inputRoomCode.value);
    });
    btnLeaveRoom.addEventListener('click', leaveRoom);
    
    // Copy codes
    btnCopyCode.addEventListener('click', showShareModal);
    btnBarTogether.addEventListener('click', () => {
        togetherPanel.classList.toggle('collapsed');
    });
    togglePanelSidebar.addEventListener('click', () => {
        togetherPanel.classList.add('collapsed');
    });
    navTogetherBtn.addEventListener('click', (e) => {
        e.preventDefault();
        togetherPanel.classList.remove('collapsed');
    });
    
    // Share Modals
    btnCloseShareModal.addEventListener('click', closeShareModal);
    shareModal.addEventListener('click', (e) => {
        if (e.target === shareModal) closeShareModal();
    });
    document.addEventListener('keydown', (e) => {
        if (e.key === 'Escape' && !shareModal.classList.contains('hidden')) {
            closeShareModal();
        }
    });
    btnCopyShareLink.addEventListener('click', () => {
        shareModalLink.select();
        document.execCommand('copy');
        showToast("已成功复制专属邀请链接！快发给QQ好友吧");
        closeShareModal();
    });
    
    // Chats send message
    chatSendForm.addEventListener('submit', (e) => {
        e.preventDefault();
        const text = chatTextInput.value;
        sendChatMessage(text);
        chatTextInput.value = "";
    });
    
    // Reaction Soundboard buttons
    const reactBtns = togetherPanel.querySelectorAll('.react-btn');
    reactBtns.forEach(btn => {
        btn.addEventListener('click', () => {
            const emoji = btn.dataset.emoji;
            sendReaction(emoji);
        });
    });

    // Favorite heart toggle
    favoriteBtn.addEventListener('click', () => {
        favoriteBtn.classList.toggle('active');
        const active = favoriteBtn.classList.contains('active');
        showToast(active ? "已添加到我喜欢" : "已取消喜欢");
    });
    
    // Menu items toggle simulation
    navRecommend.addEventListener('click', (e) => {
        e.preventDefault();
        showToast("正在为您个性化定制今日曲库...");
    });
    navRadio.addEventListener('click', (e) => {
        e.preventDefault();
        showToast("正在连线QQ音乐私人FM电台...");
    });
}

// Initializing on content loaded
window.addEventListener('DOMContentLoaded', () => {
    initPlayer();
    lucide.createIcons();
});
