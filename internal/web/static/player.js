const audios = [];

const formatTime = seconds => {
	if (!Number.isFinite(seconds)) return '0:00';
	const total = Math.floor(seconds);
	return Math.floor(total / 60) + ':' + String(total % 60).padStart(2, '0');
};

for (const root of document.querySelectorAll('[data-player]')) {
	const audio = root.querySelector('audio');
	const toggle = root.querySelector('[data-toggle]');
	const icon = toggle.querySelector('img');
	const seek = root.querySelector('[data-seek]');
	const time = root.querySelector('[data-time]');
	let seeking = false;
	audios.push(audio);

	const updateTime = () => {
		const duration = Number.isFinite(audio.duration) ? audio.duration : 0;
		seek.max = duration;
		if (!seeking) seek.value = audio.currentTime;
		time.textContent = formatTime(seeking ? parseFloat(seek.value) : audio.currentTime) + ' / ' + formatTime(duration);
	};

	const updateState = () => {
		const label = audio.paused ? 'play' : 'pause';
		icon.src = toggle.dataset[label];
		icon.alt = label;
		toggle.title = label;
		root.classList.toggle('playing', !audio.paused);
	};

	toggle.addEventListener('click', () => {
		if (!audio.paused) {
			audio.pause();
			return;
		}
		for (const other of audios) {
			if (other !== audio) other.pause();
		}
		audio.play();
	});

	seek.addEventListener('input', () => {
		seeking = true;
		updateTime();
	});
	seek.addEventListener('change', () => {
		audio.currentTime = parseFloat(seek.value);
		seeking = false;
		updateTime();
	});

	for (const event of ['timeupdate', 'durationchange', 'loadedmetadata']) {
		audio.addEventListener(event, updateTime);
	}
	for (const event of ['play', 'pause']) {
		audio.addEventListener(event, updateState);
	}
	audio.addEventListener('ended', () => {
		audio.currentTime = 0;
		updateTime();
	});
	updateTime();
	updateState();
}
