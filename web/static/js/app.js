/**
 * MinerHQ Dashboard Application
 * Frontend UI for managing and monitoring ASIC miners
 */
class MinerHQ {
    constructor() {
        this.miners = [];
        this.shares = [];
        this.sharesHistory = [];
        this.coins = [];
        this.maxShares = 10;
        this.ws = null;
        this.wsReconnectDelay = 1000;
        this.hashrateChart = null;
        this.sharesScatterChart = null;
        this.sharesHistogramChart = null;
        this.minerDetailChart = null;
        this.currentPage = 'dashboard';
        this.settings = {};
        this.currentMiner = null;
        this.modalRefreshInterval = null;
        this.sharesChartInterval = null;
        this.competitionRefreshTimeout = null;
        this.earningsCoins = [];
        this.earningsCoinIndex = 0;
        this.earningsCycleInterval = null;
        this.init();
    }

    async init() {
        this.bindEvents();
        await this.fetchSettings(); // Load settings first for energy cost calculation
        await this.fetchStats();
        await this.fetchMiners();
        await this.fetchCompetition();
        await this.fetchMoneyMakers();
        await this.loadCoins();
        await this.loadEarnings();
        this.connectWebSocket();
        this.initHashrateChart();

        setInterval(() => this.fetchStats(), 5000);
        setInterval(() => this.fetchMiners(), 30000);
        setInterval(() => this.refreshHashrateChart(), 5000);
        setInterval(() => this.fetchCompetition(), 30000);
        setInterval(() => this.fetchMoneyMakers(), 30000);
        setInterval(() => this.updateCompetitionCountdown(), 1000);
        setInterval(() => this.loadEarnings(), 60000); // Update earnings every minute
    }

    bindEvents() {
        document.querySelectorAll('.nav-tab').forEach(tab => {
            tab.addEventListener('click', () => this.switchPage(tab.dataset.page));
        });

        const scanBtn = document.getElementById('scan-btn');
        const modalClose = document.getElementById('modal-close');
        const startScanBtn = document.getElementById('start-scan-btn');
        const modal = document.getElementById('scan-modal');

        if (scanBtn) scanBtn.addEventListener('click', () => this.openScanModal());
        if (modalClose) modalClose.addEventListener('click', () => this.closeScanModal());
        if (startScanBtn) startScanBtn.addEventListener('click', () => this.scan());
        if (modal) modal.addEventListener('click', (e) => {
            if (e.target === modal) this.closeScanModal();
        });

        const manualAddBtn = document.getElementById('manual-add-btn');
        const manualIpInput = document.getElementById('manual-ip-input');
        if (manualAddBtn) manualAddBtn.addEventListener('click', () => this.addManualMiner());
        if (manualIpInput) manualIpInput.addEventListener('keydown', (e) => {
            if (e.key === 'Enter') this.addManualMiner();
        });

        const minerModalClose = document.getElementById('miner-modal-close');
        const minerModal = document.getElementById('miner-modal');
        if (minerModalClose) minerModalClose.addEventListener('click', () => this.closeMinerModal());
        if (minerModal) minerModal.addEventListener('click', (e) => {
            if (e.target === minerModal) this.closeMinerModal();
        });


        const saveSettingsBtn = document.getElementById('save-settings-btn');
        const purgeBtn = document.getElementById('purge-btn');
        if (saveSettingsBtn) saveSettingsBtn.addEventListener('click', () => this.saveSettings());
        if (purgeBtn) purgeBtn.addEventListener('click', () => this.purgeData());

        document.addEventListener('keydown', (e) => {
            if (e.key === 'Escape') {
                this.closeScanModal();
                this.closeMinerModal();
            }
        });
    }

    switchPage(page) {
        this.currentPage = page;
        document.querySelectorAll('.nav-tab').forEach(tab => {
            tab.classList.toggle('active', tab.dataset.page === page);
        });
        document.querySelectorAll('.page').forEach(p => {
            p.classList.toggle('active', p.id === 'page-' + page);
        });

        // Clear shares chart interval when leaving shares page
        if (page !== 'shares' && this.sharesChartInterval) {
            clearInterval(this.sharesChartInterval);
            this.sharesChartInterval = null;
        }

        if (page === 'shares') this.loadSharesPage();
        else if (page === 'settings') this.loadSettingsPage();
    }

    async fetchStats() {
        try {
            const [statsRes, blocksRes] = await Promise.all([
                fetch('/api/stats'),
                fetch('/api/blocks/count')
            ]);

            if (statsRes.ok) {
                const stats = await statsRes.json();
                this.updateSummaryCards(stats);
            }

            if (blocksRes.ok) {
                const blocks = await blocksRes.json();
                this.updateBlockCount(blocks.count);
            }
        } catch (error) {
            console.error('Error fetching stats:', error);
        }
    }

    updateSummaryCards(stats) {
        const hashrateEl = document.getElementById('total-hashrate');
        const hashrateSubEl = document.getElementById('hashrate-subtitle');
        const powerEl = document.getElementById('total-power');
        const powerSubEl = document.getElementById('power-subtitle');
        const fleetEl = document.getElementById('fleet-status');

        if (hashrateEl) {
            hashrateEl.textContent = this.formatHashrate((stats.totalHashrate || 0) * 1e9);
        }

        if (hashrateSubEl) {
            hashrateSubEl.textContent = (stats.onlineMiners || 0) + ' miners active';
        }

        if (powerEl) {
            powerEl.textContent = this.formatPower(stats.totalPower || 0);
        }

        // Calculate and display daily energy cost
        if (powerSubEl) {
            const powerWatts = stats.totalPower || 0;
            const costPerKwh = this.settings?.energy?.cost_per_kwh || 0;
            const currency = this.settings?.energy?.currency || 'USD';

            if (costPerKwh > 0 && powerWatts > 0) {
                // Daily cost = (W / 1000) * 24h * cost_per_kwh
                const dailyCost = (powerWatts / 1000) * 24 * costPerKwh;
                const currencySymbol = this.getCurrencySymbol(currency);
                powerSubEl.textContent = `≈ ${currencySymbol}${dailyCost.toFixed(2)}/day`;
            } else {
                powerSubEl.textContent = 'estimated';
            }
        }

        if (fleetEl) {
            fleetEl.textContent = '';
            const onlineSpan = document.createElement('span');
            onlineSpan.className = 'status-online';
            onlineSpan.textContent = stats.onlineMiners || 0;

            const separator = document.createTextNode(' / ');

            const offlineSpan = document.createElement('span');
            offlineSpan.className = 'status-offline';
            offlineSpan.textContent = (stats.totalMiners || 0) - (stats.onlineMiners || 0);

            fleetEl.appendChild(onlineSpan);
            fleetEl.appendChild(separator);
            fleetEl.appendChild(offlineSpan);
        }
    }

    updateBlockCount(count) {
        const blocksEl = document.getElementById('total-blocks');
        if (blocksEl) {
            blocksEl.textContent = count || 0;
        }
    }

    // Weekly Competition
    async fetchCompetition() {
        try {
            const response = await fetch('/api/competition/weekly');
            if (!response.ok) throw new Error('Failed to fetch competition');

            const oldCompetition = this.competition;
            this.competition = await response.json();

            // Check if data changed for visual feedback
            const hasChanged = this.competitionHasChanged(oldCompetition, this.competition);
            this.renderCompetition(hasChanged);
        } catch (error) {
            console.error('Error fetching competition:', error);
        }
    }

    competitionHasChanged(oldData, newData) {
        if (!oldData || !newData) return false;
        if (!oldData.competitors || !newData.competitors) return false;
        if (oldData.competitors.length !== newData.competitors.length) return true;

        for (let i = 0; i < newData.competitors.length; i++) {
            const oldC = oldData.competitors[i];
            const newC = newData.competitors[i];
            if (!oldC || oldC.minerIp !== newC.minerIp || oldC.bestDiff !== newC.bestDiff) {
                return true;
            }
        }
        return false;
    }

    renderCompetition(hasChanged = false) {
        if (!this.competition) return;

        const competitors = this.competition.competitors || [];

        // Update countdown
        const countdownEl = document.getElementById('competition-countdown');
        if (countdownEl) {
            countdownEl.textContent = this.competition.timeRemaining;
        }

        // Handle empty state
        const sectionEl = document.querySelector('.competition-section');
        const podiumEl = document.getElementById('competition-podium');
        const listEl = document.getElementById('competition-list');
        const achievementsEl = document.getElementById('competition-achievements');

        // Visual feedback when data changes
        if (hasChanged && sectionEl) {
            sectionEl.classList.remove('updating');
            void sectionEl.offsetWidth; // Trigger reflow
            sectionEl.classList.add('updating');
        }

        if (competitors.length === 0) {
            if (podiumEl) podiumEl.style.display = 'none';
            if (listEl) listEl.innerHTML = `
                <div class="competition-empty">
                    <div class="competition-empty-icon">⏳</div>
                    <div>No shares recorded this week yet.</div>
                    <div style="font-size: 0.8rem; margin-top: 0.5rem;">Start mining to compete!</div>
                </div>
            `;
            if (achievementsEl) achievementsEl.innerHTML = '';
            return;
        }

        if (podiumEl) podiumEl.style.display = 'flex';

        // Render podium (top 3)
        this.renderPodium(competitors.slice(0, 3), hasChanged);

        // Render list (4th and below)
        this.renderCompetitorList(competitors.slice(3));

        // Render achievements
        this.renderAchievements(competitors);

        // Render block competition
        this.renderBlockCompetition(hasChanged);
    }

    renderBlockCompetition(hasChanged = false) {
        const blockCompetitors = this.competition?.blockCompetitors || [];
        const leaderboardEl = document.getElementById('block-leaderboard');
        if (!leaderboardEl) return;

        // Visual feedback when data changes
        const sectionEl = document.querySelector('.block-competition');
        if (hasChanged && sectionEl) {
            sectionEl.classList.remove('updating');
            void sectionEl.offsetWidth;
            sectionEl.classList.add('updating');
        }

        // Empty state
        if (blockCompetitors.length === 0) {
            leaderboardEl.innerHTML = `
                <div class="block-empty">
                    <div class="block-empty-icon">⛏️</div>
                    <div>No blocks found yet.</div>
                    <div style="font-size: 0.8rem; margin-top: 0.5rem;">Be the first to find one!</div>
                </div>
            `;
            return;
        }

        leaderboardEl.innerHTML = blockCompetitors.map((c, index) => {
            const rankClass = index === 0 ? 'gold' : index === 1 ? 'silver' : index === 2 ? 'bronze' : '';
            const rowClass = index === 0 ? 'top-hunter' : '';
            const streakClass = c.streak >= 3 ? 'hot' : '';

            // Title display
            const titleHtml = c.title ? `
                <div class="block-hunter-title">
                    <span class="block-hunter-title-icon">${c.titleIcon}</span>
                    <span class="block-hunter-title-text">${c.title}</span>
                </div>
            ` : '';

            // Block badge with counter
            const badgeHtml = c.blocksThisWeek > 0 ? `
                <span class="block-badge">
                    <span class="block-badge-icon">⛏️</span>
                    x${c.blocksThisWeek}
                </span>
            ` : '';

            return `
                <div class="block-hunter-row ${rowClass}">
                    <div class="block-hunter-rank ${rankClass}">#${c.rank}</div>
                    <div class="block-hunter-info">
                        <span class="block-hunter-name">${c.hostname || c.minerIp} ${badgeHtml}</span>
                        ${titleHtml}
                    </div>
                    <div class="block-hunter-stats">
                        <div class="block-hunter-weekly">${c.blocksThisWeek}</div>
                        <div class="block-hunter-weekly-label">This Week</div>
                    </div>
                    <div class="block-hunter-alltime">
                        <div class="block-hunter-alltime-value">${c.blocksAllTime}</div>
                        <div class="block-hunter-alltime-label">All-Time</div>
                    </div>
                    <div class="block-hunter-streak ${streakClass}">
                        <div class="block-hunter-streak-value">🔥 ${c.streak}</div>
                        <div class="block-hunter-streak-label">Streak</div>
                    </div>
                </div>
            `;
        }).join('');
    }

    // Money Makers Competition
    async fetchMoneyMakers() {
        try {
            const response = await fetch('/api/competition/moneymakers');
            if (!response.ok) throw new Error('Failed to fetch money makers');

            this.moneyMakers = await response.json();
            this.renderMoneyMakers();
        } catch (error) {
            console.error('Error fetching money makers:', error);
        }
    }

    renderMoneyMakers() {
        const competitors = this.moneyMakers?.competitors || [];
        const leaderboardEl = document.getElementById('money-makers-leaderboard');
        if (!leaderboardEl) return;

        // Empty state
        if (competitors.length === 0) {
            leaderboardEl.innerHTML = `
                <div class="block-empty">
                    <div class="block-empty-icon">💰</div>
                    <div>No earnings recorded yet.</div>
                    <div style="font-size: 0.8rem; margin-top: 0.5rem;">Find a block to start earning!</div>
                </div>
            `;
            return;
        }

        leaderboardEl.innerHTML = competitors.map((c, index) => {
            const rankClass = index === 0 ? 'gold' : index === 1 ? 'silver' : index === 2 ? 'bronze' : '';
            const rowClass = index === 0 ? 'top-earner' : '';

            // Title display
            const titleHtml = c.title ? `
                <div class="money-maker-title">
                    <span class="money-maker-title-icon">${c.titleIcon}</span>
                    <span class="money-maker-title-text">${c.title}</span>
                </div>
            ` : '';

            // Weekly badge - show current value
            const weeklyBadgeHtml = c.weeklyCurrentUsd > 0 ? `
                <span class="weekly-badge">
                    +$${this.formatUSD(c.weeklyCurrentUsd)} this week
                </span>
            ` : '';

            // Calculate value change percentage
            const valueChange = c.totalUsd > 0 ? ((c.currentUsd - c.totalUsd) / c.totalUsd * 100) : 0;
            const changeClass = valueChange >= 0 ? 'positive' : 'negative';
            const changeIcon = valueChange >= 0 ? '▲' : '▼';

            return `
                <div class="money-maker-row ${rowClass}">
                    <div class="money-maker-rank ${rankClass}">#${c.rank}</div>
                    <div class="money-maker-info">
                        <span class="money-maker-name">${c.hostname || c.minerIp} ${weeklyBadgeHtml}</span>
                        ${titleHtml}
                    </div>
                    <div class="money-maker-stats">
                        <div class="money-maker-blocks">${c.blockCount} blocks</div>
                    </div>
                    <div class="money-maker-earned">
                        <div class="money-maker-value">$${this.formatUSD(c.totalUsd)}</div>
                        <div class="money-maker-label">Earned</div>
                    </div>
                    <div class="money-maker-current">
                        <div class="money-maker-value ${changeClass}">$${this.formatUSD(c.currentUsd)}</div>
                        <div class="money-maker-label">Current <span class="change-indicator ${changeClass}">${changeIcon}${Math.abs(valueChange).toFixed(1)}%</span></div>
                    </div>
                </div>
            `;
        }).join('');
    }

    renderPodium(top3, hasChanged = false) {
        const positions = [
            { id: 'podium-1', index: 0 },
            { id: 'podium-2', index: 1 },
            { id: 'podium-3', index: 2 }
        ];

        positions.forEach(pos => {
            const el = document.getElementById(pos.id);
            if (!el) return;

            const competitor = top3[pos.index];
            const nameEl = el.querySelector('.podium-name');
            const diffEl = el.querySelector('.podium-diff');

            // Remove existing legend badge if any
            const existingBadge = el.querySelector('.legend-badge');
            if (existingBadge) existingBadge.remove();

            // Remove legend class
            el.classList.remove('miner-legend');

            if (competitor) {
                nameEl.textContent = competitor.hostname || competitor.minerIp;
                diffEl.textContent = this.formatDifficulty(competitor.bestDiff);

                // Add new record indicator
                if (competitor.isNewRecord) {
                    diffEl.innerHTML = this.formatDifficulty(competitor.bestDiff) +
                        ' <span class="new-record-badge">NEW RECORD!</span>';
                }

                // MINER LEGEND - found a block this week
                if (competitor.foundBlockThisWeek) {
                    el.classList.add('miner-legend');
                    const badge = document.createElement('div');
                    badge.className = 'legend-badge';
                    badge.innerHTML = '<span class="legend-badge-icon">⛏️</span> MINER LEGEND';
                    el.appendChild(badge);
                }

                el.style.display = 'flex';
                el.style.opacity = '1';

                // Flash animation when updated
                if (hasChanged) {
                    el.classList.remove('updated');
                    void el.offsetWidth; // Trigger reflow
                    el.classList.add('updated');
                }
            } else {
                nameEl.textContent = '--';
                diffEl.textContent = '--';
                el.style.opacity = '0.3';
            }
        });
    }

    renderCompetitorList(competitors) {
        const listEl = document.getElementById('competition-list');
        if (!listEl) return;

        if (competitors.length === 0) {
            listEl.innerHTML = '';
            return;
        }

        listEl.innerHTML = competitors.map(c => {
            const isOnFire = c.rankChange > 0;
            const isLegend = c.foundBlockThisWeek;
            const rankChangeHtml = c.rankChange > 0
                ? `<span class="rank-up">▲${c.rankChange}</span>`
                : c.rankChange < 0
                    ? `<span class="rank-down">▼${Math.abs(c.rankChange)}</span>`
                    : '';
            const newRecordHtml = c.isNewRecord
                ? '<span class="new-record-badge">NEW RECORD!</span>'
                : '';
            const legendBadgeHtml = isLegend
                ? '<span class="legend-badge" style="margin-left: 8px;"><span class="legend-badge-icon">⛏️</span> MINER LEGEND</span>'
                : '';

            const classes = ['competitor-row'];
            if (isOnFire) classes.push('on-fire');
            if (isLegend) classes.push('miner-legend');

            return `
                <div class="${classes.join(' ')}">
                    <div class="competitor-rank">#${c.rank}${rankChangeHtml}</div>
                    <div class="competitor-info">
                        <span class="competitor-name">${c.hostname || c.minerIp}${newRecordHtml}${legendBadgeHtml}</span>
                        <span class="competitor-shares">${c.shareCount} shares this week</span>
                    </div>
                    <div class="competitor-diff">${this.formatDifficulty(c.bestDiff)}</div>
                    <div class="competitor-progress">
                        <div class="competitor-progress-bar" style="width: ${c.percentOfTop}%"></div>
                    </div>
                </div>
            `;
        }).join('');
    }

    renderAchievements(competitors) {
        const achievementsEl = document.getElementById('competition-achievements');
        if (!achievementsEl) return;

        const achievements = [];

        // MINER LEGEND - found a block this week (top priority)
        const legends = competitors.filter(c => c.foundBlockThisWeek);
        legends.forEach(c => {
            achievements.push({
                icon: '⛏️',
                text: 'Miner Legend',
                name: c.hostname || c.minerIp,
                class: 'legend'
            });
        });

        // Current champion
        if (competitors.length > 0) {
            achievements.push({
                icon: '👑',
                text: 'Leading',
                name: competitors[0].hostname || competitors[0].minerIp,
                class: 'gold'
            });
        }

        // New record holders
        const recordHolders = competitors.filter(c => c.isNewRecord);
        recordHolders.forEach(c => {
            achievements.push({
                icon: '⚡',
                text: 'New Record',
                name: c.hostname || c.minerIp,
                class: 'gold'
            });
        });

        // Most active (most shares)
        if (competitors.length > 0) {
            const mostActive = [...competitors].sort((a, b) => b.shareCount - a.shareCount)[0];
            achievements.push({
                icon: '🎯',
                text: 'Most Active',
                name: mostActive.hostname || mostActive.minerIp,
                class: 'silver'
            });
        }

        achievementsEl.innerHTML = achievements.map(a => `
            <div class="achievement-badge ${a.class}">
                <span class="achievement-icon">${a.icon}</span>
                <span class="achievement-text">${a.text}:</span>
                <span class="achievement-name">${a.name}</span>
            </div>
        `).join('');
    }

    updateCompetitionCountdown() {
        if (!this.competition || !this.competition.secondsLeft) return;

        this.competition.secondsLeft--;
        if (this.competition.secondsLeft < 0) {
            this.fetchCompetition(); // Refresh when week ends
            return;
        }

        const seconds = this.competition.secondsLeft;
        const days = Math.floor(seconds / 86400);
        const hours = Math.floor((seconds % 86400) / 3600);
        const minutes = Math.floor((seconds % 3600) / 60);
        const secs = seconds % 60;

        let timeStr;
        if (days > 0) {
            timeStr = `${days}d ${hours}h ${minutes}m`;
        } else if (hours > 0) {
            timeStr = `${hours}h ${minutes}m ${secs}s`;
        } else {
            timeStr = `${minutes}m ${secs}s`;
        }

        const countdownEl = document.getElementById('competition-countdown');
        if (countdownEl) {
            countdownEl.textContent = timeStr;
        }
    }

    async fetchMiners() {
        try {
            const response = await fetch('/api/miners');
            if (!response.ok) throw new Error('Failed to fetch miners');
            this.miners = await response.json();
            this.renderMiners();
            this.updateMinerFilter();
        } catch (error) {
            console.error('Error fetching miners:', error);
        }
    }

    renderMiners() {
        const grid = document.getElementById('miners-grid');
        if (!grid) return;

        grid.textContent = '';

        if (this.miners.length === 0) {
            const emptyState = document.createElement('div');
            emptyState.className = 'empty-state';
            emptyState.textContent = 'No miners configured. Click Scan to discover miners on your network.';
            grid.appendChild(emptyState);
            return;
        }

        this.miners.forEach(miner => {
            const card = this.createMinerCard(miner);
            grid.appendChild(card);
        });
    }

    createMinerCard(miner) {
        const card = document.createElement('div');
        card.className = 'miner-card';
        card.dataset.ip = miner.ip;

        const isOnline = miner.online !== false;
        if (!isOnline) card.classList.add('offline');

        card.addEventListener('click', () => this.openMinerModal(miner));

        const header = document.createElement('div');
        header.className = 'miner-header';

        const name = document.createElement('span');
        name.className = 'miner-name';
        name.textContent = miner.hostname || miner.ip;

        const status = document.createElement('div');
        status.className = 'miner-status';

        const statusDot = document.createElement('span');
        statusDot.className = 'status-dot';
        if (!isOnline) statusDot.classList.add('offline');

        const statusText = document.createElement('span');
        statusText.textContent = isOnline ? 'ONLINE' : 'OFFLINE';

        status.appendChild(statusDot);
        status.appendChild(statusText);
        header.appendChild(name);
        header.appendChild(status);

        const ipDiv = document.createElement('div');
        ipDiv.className = 'miner-ip';
        ipDiv.textContent = miner.ip;

        const stats = document.createElement('div');
        stats.className = 'miner-stats';

        const snapshot = miner.snapshot || {};
        const hashrateGHs = snapshot.hashRate || 0;

        stats.appendChild(this.createStatItem('HASHRATE', this.formatHashrate(hashrateGHs * 1e9)));
        stats.appendChild(this.createStatItem('POWER', this.formatPower(snapshot.power || 0), 'power'));
        stats.appendChild(this.createStatItem('TEMP', this.formatTemp(snapshot.temperature || 0), 'temp'));
        stats.appendChild(this.createStatItem('MODEL', miner.deviceModel || miner.asicModel || 'Unknown'));

        card.appendChild(header);
        card.appendChild(ipDiv);
        card.appendChild(stats);

        return card;
    }

    createStatItem(label, value, valueClass) {
        const item = document.createElement('div');
        item.className = 'stat-item';

        const labelEl = document.createElement('span');
        labelEl.className = 'stat-label';
        labelEl.textContent = label;

        const valueEl = document.createElement('span');
        valueEl.className = 'stat-value';
        if (valueClass) valueEl.classList.add(valueClass);
        valueEl.textContent = value;

        item.appendChild(labelEl);
        item.appendChild(valueEl);
        return item;
    }

    async openMinerModal(miner) {
        const modal = document.getElementById('miner-modal');
        const title = document.getElementById('miner-modal-title');
        const body = document.getElementById('miner-modal-body');

        if (!modal || !body) return;

        // Store current miner for real-time updates
        this.currentMiner = miner;

        title.textContent = miner.hostname || miner.ip;
        body.textContent = '';
        const loading = document.createElement('div');
        loading.className = 'empty-state';
        loading.textContent = 'Loading...';
        body.appendChild(loading);
        modal.classList.remove('hidden');

        // Initial load
        await this.refreshMinerModal();

        // Set up real-time refresh every 2 seconds
        this.modalRefreshInterval = setInterval(() => this.refreshMinerModal(), 2000);
    }

    async refreshMinerModal() {
        if (!this.currentMiner) return;

        try {
            // Fetch latest miner data
            const [minerRes, historyRes] = await Promise.all([
                fetch('/api/miners/' + this.currentMiner.ip),
                fetch('/api/miners/' + this.currentMiner.ip + '/history?hours=1&limit=500')
            ]);

            if (minerRes.ok) {
                const minerData = await minerRes.json();
                // Merge updated fields (including coinId)
                Object.assign(this.currentMiner, minerData);
                // Get latest snapshot
                const snapshotsRes = await fetch('/api/miners/' + this.currentMiner.ip + '/history?hours=1&limit=1');
                if (snapshotsRes.ok) {
                    const snapshots = await snapshotsRes.json();
                    if (snapshots.length > 0) {
                        this.currentMiner.snapshot = snapshots[0];
                    }
                }
            }

            const history = historyRes.ok ? await historyRes.json() : [];
            this.renderMinerDetail(this.currentMiner, history);
        } catch (error) {
            console.error('Error refreshing miner modal:', error);
        }
    }

    renderMinerDetail(miner, history) {
        const body = document.getElementById('miner-modal-body');
        if (!body) return;

        const snapshot = miner.snapshot || (history.length > 0 ? history[0] : {});

        // Check if we need to create the DOM or just update values
        const existingGrid = body.querySelector('.miner-detail-grid');
        if (existingGrid) {
            // Just update the values without recreating DOM
            this.updateMinerDetailValues(snapshot, miner);
            this.updateMinerDetailChart(history);
            return;
        }

        // First time - create the DOM structure
        body.textContent = '';

        const grid = document.createElement('div');
        grid.className = 'miner-detail-grid';

        // Hashrate section
        grid.appendChild(this.createDetailSection('HASHRATE', [
            ['Current', this.formatHashrate((snapshot.hashRate || 0) * 1e9), '', 'detail-hashrate'],
            ['1 Minute', this.formatHashrate((snapshot.hashRate1m || 0) * 1e9), '', 'detail-hashrate1m'],
            ['1 Hour', this.formatHashrate((snapshot.hashRate1h || 0) * 1e9), '', 'detail-hashrate1h'],
            ['1 Day', this.formatHashrate((snapshot.hashRate1d || 0) * 1e9), '', 'detail-hashrate1d']
        ]));

        // Network section
        const wifiClass = snapshot.wifiRssi < -70 ? 'warning' : '';
        grid.appendChild(this.createDetailSection('NETWORK', [
            ['IP Address', miner.ip],
            ['WiFi Signal', (snapshot.wifiRssi || 0) + ' dBm', wifiClass, 'detail-wifi'],
            ['Uptime', this.formatUptime(snapshot.uptimeSeconds || 0), '', 'detail-uptime']
        ]));

        // Pool section
        const poolClass = snapshot.poolConnected ? 'success' : 'danger';
        grid.appendChild(this.createDetailSection('POOL', [
            ['Status', snapshot.poolConnected ? 'Connected' : 'Disconnected', poolClass, 'detail-pool-status'],
            ['Difficulty', this.formatDifficulty(snapshot.poolDifficulty || 0), '', 'detail-difficulty'],
            ['Best Session', this.formatDifficulty(snapshot.bestDiffSession || 0), '', 'detail-best-diff'],
            ['Shares', (snapshot.sharesAccepted || 0) + ' / ' + (snapshot.sharesRejected || 0), '', 'detail-shares']
        ]));

        // Power section
        const efficiency = snapshot.hashRate > 0 ? ((snapshot.power / snapshot.hashRate) * 1000).toFixed(1) : 0;
        grid.appendChild(this.createDetailSection('POWER', [
            ['Power', this.formatPower(snapshot.power || 0), '', 'detail-power'],
            ['Voltage', ((snapshot.voltage || 0) / 1000).toFixed(2) + ' V', '', 'detail-voltage'],
            ['Efficiency', efficiency + ' J/TH', '', 'detail-efficiency']
        ]));

        // Cooling section
        const tempClass = snapshot.temperature > 70 ? 'danger' : (snapshot.temperature > 60 ? 'warning' : '');
        const fanClass = snapshot.fanRpm < 1000 ? 'warning' : '';
        grid.appendChild(this.createDetailSection('COOLING', [
            ['Chip Temp', this.formatTemp(snapshot.temperature || 0), tempClass, 'detail-temp'],
            ['VR Temp', this.formatTemp(snapshot.vrTemp || 0), '', 'detail-vrtemp'],
            ['Fan RPM', String(snapshot.fanRpm || 0), fanClass, 'detail-fanrpm'],
            ['Fan %', (snapshot.fanPercent || 0) + '%', '', 'detail-fanpct']
        ]));

        // Device section
        grid.appendChild(this.createDetailSection('DEVICE', [
            ['Model', miner.deviceModel || 'Unknown'],
            ['ASIC', miner.asicModel || 'Unknown']
        ]));

        // Mining coin section
        const coinSection = document.createElement('div');
        coinSection.className = 'detail-section';
        const coinH4 = document.createElement('h4');
        coinH4.textContent = 'MINING';
        coinSection.appendChild(coinH4);

        const coinRow = document.createElement('div');
        coinRow.className = 'detail-row';
        const coinLabel = document.createElement('span');
        coinLabel.className = 'detail-label';
        coinLabel.textContent = 'Coin';
        const coinSelect = document.createElement('select');
        coinSelect.id = 'miner-coin-select';
        coinSelect.className = 'detail-select';

        // Default option = use DGB as fallback
        const defaultOpt = document.createElement('option');
        defaultOpt.value = '';
        defaultOpt.textContent = 'Default (DGB)';
        coinSelect.appendChild(defaultOpt);

        if (this.coins) {
            this.coins.forEach(coin => {
                const opt = document.createElement('option');
                opt.value = coin.id;
                opt.textContent = `${coin.symbol} - ${coin.name}`;
                coinSelect.appendChild(opt);
            });
        }
        coinSelect.value = miner.coinId || '';

        coinSelect.addEventListener('change', async () => {
            try {
                const res = await fetch(`/api/miners/${miner.ip}/coin`, {
                    method: 'PUT',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ coin: coinSelect.value })
                });
                if (res.ok) {
                    this.loadEarnings();
                } else {
                    console.error('Failed to set miner coin');
                }
            } catch (e) {
                console.error('Error setting miner coin:', e);
            }
        });

        coinRow.appendChild(coinLabel);
        coinRow.appendChild(coinSelect);
        coinSection.appendChild(coinRow);
        grid.appendChild(coinSection);

        // Chart container with legend
        const chartContainer = document.createElement('div');
        chartContainer.className = 'miner-chart-container';

        // Chart header with legend
        const chartHeader = document.createElement('div');
        chartHeader.className = 'chart-header';
        chartHeader.innerHTML = `
            <h3>HASHRATE HISTORY</h3>
            <div class="chart-legend">
                <div class="legend-item"><span class="legend-color" style="background-color: #a564f6;"></span> Hashrate</div>
                <div class="legend-item"><span class="legend-color" style="border: 1px dashed #a564f6; border-style: dashed;"></span> 10min</div>
                <div class="legend-item"><span class="legend-color" style="border: 2px dashed #a564f6;"></span> 1h</div>
                <div class="legend-item"><span class="legend-color" style="background-color: #2DA8B7;"></span> VReg</div>
                <div class="legend-item"><span class="legend-color" style="background-color: #C84847;"></span> ASIC</div>
            </div>
        `;
        chartContainer.appendChild(chartHeader);

        const canvas = document.createElement('canvas');
        canvas.id = 'miner-detail-chart';
        chartContainer.appendChild(canvas);
        grid.appendChild(chartContainer);

        body.appendChild(grid);

        this.createMinerDetailChart(history);
    }

    updateMinerDetailValues(snapshot, miner) {
        const updates = {
            'detail-hashrate': this.formatHashrate((snapshot.hashRate || 0) * 1e9),
            'detail-hashrate1m': this.formatHashrate((snapshot.hashRate1m || 0) * 1e9),
            'detail-hashrate1h': this.formatHashrate((snapshot.hashRate1h || 0) * 1e9),
            'detail-hashrate1d': this.formatHashrate((snapshot.hashRate1d || 0) * 1e9),
            'detail-wifi': (snapshot.wifiRssi || 0) + ' dBm',
            'detail-uptime': this.formatUptime(snapshot.uptimeSeconds || 0),
            'detail-pool-status': snapshot.poolConnected ? 'Connected' : 'Disconnected',
            'detail-difficulty': this.formatDifficulty(snapshot.poolDifficulty || 0),
            'detail-best-diff': this.formatDifficulty(snapshot.bestDiffSession || 0),
            'detail-shares': (snapshot.sharesAccepted || 0) + ' / ' + (snapshot.sharesRejected || 0),
            'detail-power': this.formatPower(snapshot.power || 0),
            'detail-voltage': ((snapshot.voltage || 0) / 1000).toFixed(2) + ' V',
            'detail-efficiency': snapshot.hashRate > 0 ? ((snapshot.power / snapshot.hashRate) * 1000).toFixed(1) + ' J/TH' : '0 J/TH',
            'detail-temp': this.formatTemp(snapshot.temperature || 0),
            'detail-vrtemp': this.formatTemp(snapshot.vrTemp || 0),
            'detail-fanrpm': String(snapshot.fanRpm || 0),
            'detail-fanpct': (snapshot.fanPercent || 0) + '%'
        };

        for (const [id, value] of Object.entries(updates)) {
            const el = document.getElementById(id);
            if (el) el.textContent = value;
        }

        // Keep coin select in sync (don't overwrite if user is actively changing it)
        const coinSelect = document.getElementById('miner-coin-select');
        if (coinSelect && document.activeElement !== coinSelect) {
            coinSelect.value = miner.coinId || '';
        }
    }

    updateMinerDetailChart(history) {
        if (!this.minerDetailChart) return;

        const reversed = [...history].reverse();
        const labels = reversed.map(h => new Date(h.timestamp));
        const hr1m = reversed.map(h => h.hashRate1m || h.hashRate || 0);
        const hr10m = reversed.map(h => h.hashRate10m || 0);
        const hr1h = reversed.map(h => h.hashRate1h || 0);
        const tempVreg = reversed.map(h => h.vrTemp || 0);
        const tempAsic = reversed.map(h => h.temperature || 0);

        this.minerDetailChart.data.labels = labels;
        this.minerDetailChart.data.datasets[0].data = hr1m;
        this.minerDetailChart.data.datasets[1].data = hr10m;
        this.minerDetailChart.data.datasets[2].data = hr1h;
        this.minerDetailChart.data.datasets[3].data = tempVreg;
        this.minerDetailChart.data.datasets[4].data = tempAsic;

        // Update scales
        this.updateMinerDetailChartScales(hr1m, hr10m, hr1h, tempVreg, tempAsic);
    }

    createDetailSection(title, rows) {
        const section = document.createElement('div');
        section.className = 'detail-section';

        const h4 = document.createElement('h4');
        h4.textContent = title;
        section.appendChild(h4);

        rows.forEach(([label, value, valueClass, valueId]) => {
            const row = document.createElement('div');
            row.className = 'detail-row';

            const labelSpan = document.createElement('span');
            labelSpan.className = 'detail-label';
            labelSpan.textContent = label;

            const valueSpan = document.createElement('span');
            valueSpan.className = 'detail-value';
            if (valueClass) valueSpan.classList.add(valueClass);
            if (valueId) valueSpan.id = valueId;
            valueSpan.textContent = value;

            row.appendChild(labelSpan);
            row.appendChild(valueSpan);
            section.appendChild(row);
        });

        return section;
    }

    createMinerDetailChart(history) {
        const ctx = document.getElementById('miner-detail-chart');
        if (!ctx) return;

        if (this.minerDetailChart) this.minerDetailChart.destroy();

        const reversed = [...history].reverse();
        const labels = reversed.map(h => new Date(h.timestamp));
        const hr1m = reversed.map(h => h.hashRate1m || h.hashRate || 0);
        const hr10m = reversed.map(h => h.hashRate10m || 0);
        const hr1h = reversed.map(h => h.hashRate1h || 0);
        const tempVreg = reversed.map(h => h.vrTemp || 0);
        const tempAsic = reversed.map(h => h.temperature || 0);

        // Create gradient for hashrate fill
        const gradient = ctx.getContext('2d').createLinearGradient(0, 0, 0, 200);
        gradient.addColorStop(0, 'rgba(165, 100, 246, 0.3)');
        gradient.addColorStop(1, 'rgba(165, 100, 246, 0.0)');

        this.minerDetailChart = new Chart(ctx, {
            type: 'line',
            data: {
                labels: labels,
                datasets: [
                    {
                        label: 'Hashrate',
                        data: hr1m,
                        borderColor: '#a564f6',
                        backgroundColor: gradient,
                        fill: true,
                        tension: 0.6,
                        cubicInterpolationMode: 'monotone',
                        pointRadius: 0,
                        borderWidth: 2.1,
                        yAxisID: 'y'
                    },
                    {
                        label: '10min avg',
                        data: hr10m,
                        borderColor: '#a564f6',
                        borderDash: [1, 4],
                        fill: false,
                        tension: 0.4,
                        pointRadius: 0,
                        borderWidth: 1.5,
                        yAxisID: 'y'
                    },
                    {
                        label: '1h avg',
                        data: hr1h,
                        borderColor: '#a564f6',
                        borderDash: [8, 2],
                        fill: false,
                        tension: 0.4,
                        pointRadius: 0,
                        borderWidth: 1,
                        yAxisID: 'y'
                    },
                    {
                        label: 'VReg',
                        data: tempVreg,
                        borderColor: '#2DA8B7',
                        fill: false,
                        tension: 0.4,
                        pointRadius: 0,
                        borderWidth: 1.5,
                        yAxisID: 'y1'
                    },
                    {
                        label: 'ASIC',
                        data: tempAsic,
                        borderColor: '#C84847',
                        fill: false,
                        tension: 0.4,
                        pointRadius: 0,
                        borderWidth: 1.5,
                        yAxisID: 'y1'
                    }
                ]
            },
            options: {
                responsive: true,
                maintainAspectRatio: false,
                animation: false,
                interaction: {
                    mode: 'index',
                    intersect: false
                },
                plugins: { legend: { display: false } },
                scales: {
                    x: {
                        type: 'time',
                        time: {
                            unit: 'minute',
                            stepSize: 15,
                            displayFormats: { minute: 'HH:mm' }
                        },
                        grid: { color: 'rgba(255,255,255,0.1)' },
                        ticks: {
                            color: '#8892a0',
                            maxTicksLimit: 5
                        }
                    },
                    y: {
                        type: 'linear',
                        position: 'left',
                        grid: { color: 'rgba(255,255,255,0.1)' },
                        ticks: {
                            color: '#a564f6',
                            maxTicksLimit: 5,
                            callback: (v) => (v / 1000).toFixed(2) + ' TH/s'
                        }
                    },
                    y1: {
                        type: 'linear',
                        position: 'right',
                        grid: { drawOnChartArea: false },
                        ticks: {
                            color: '#C84847',
                            maxTicksLimit: 5,
                            callback: (v) => v.toFixed(0) + ' °C'
                        },
                        min: 30,
                        max: 60
                    }
                }
            }
        });

        // Update Y-axis ranges based on data
        this.updateMinerDetailChartScales(hr1m, hr10m, hr1h, tempVreg, tempAsic);
    }

    updateMinerDetailChartScales(hr1m, hr10m, hr1h, tempVreg, tempAsic) {
        if (!this.minerDetailChart) return;

        // Calculate hashrate range
        const allHashrates = [...hr1m, ...hr10m.filter(v => v > 0), ...hr1h.filter(v => v > 0)].filter(v => v > 0);
        if (allHashrates.length > 0) {
            const minHash = Math.min(...allHashrates);
            const maxHash = Math.max(...allHashrates);
            const range = maxHash - minHash;
            const padding = range * 0.15;
            this.minerDetailChart.options.scales.y.min = Math.max(0, minHash - padding);
            this.minerDetailChart.options.scales.y.max = maxHash + padding;
        }

        // Calculate temperature range (base: 30-60, expands if needed)
        const allTemps = [...tempVreg, ...tempAsic].filter(v => v > 0);
        if (allTemps.length > 0) {
            const minTemp = Math.min(...allTemps);
            const maxTemp = Math.max(...allTemps);
            this.minerDetailChart.options.scales.y1.min = Math.min(30, Math.floor(minTemp - 5));
            this.minerDetailChart.options.scales.y1.max = Math.max(60, Math.ceil(maxTemp + 5));
        }

        this.minerDetailChart.update('none');
    }

    closeMinerModal() {
        // Clear real-time refresh interval
        if (this.modalRefreshInterval) {
            clearInterval(this.modalRefreshInterval);
            this.modalRefreshInterval = null;
        }
        this.currentMiner = null;

        const modal = document.getElementById('miner-modal');
        if (modal) modal.classList.add('hidden');
    }

    initHashrateChart() {
        const ctx = document.getElementById('hashrate-chart');
        if (!ctx) return;

        // Create gradient for hashrate fill (matching ESP-Miner-NerdQAxePlus style)
        const gradient = ctx.getContext('2d').createLinearGradient(0, 0, 0, 300);
        gradient.addColorStop(0, 'rgba(165, 100, 246, 0.3)');
        gradient.addColorStop(1, 'rgba(165, 100, 246, 0.0)');

        this.hashrateChart = new Chart(ctx, {
            type: 'line',
            data: {
                labels: [],
                datasets: [
                    {
                        label: 'Hashrate',
                        data: [],
                        borderColor: '#a564f6',
                        backgroundColor: gradient,
                        fill: true,
                        tension: 0.6,
                        cubicInterpolationMode: 'monotone',
                        pointRadius: 0,
                        borderWidth: 2.1,
                        yAxisID: 'y'
                    },
                    {
                        label: '10min avg',
                        data: [],
                        borderColor: '#a564f6',
                        borderDash: [1, 4],
                        fill: false,
                        tension: 0.4,
                        pointRadius: 0,
                        borderWidth: 1.5,
                        yAxisID: 'y'
                    },
                    {
                        label: '1h avg',
                        data: [],
                        borderColor: '#a564f6',
                        borderDash: [8, 2],
                        fill: false,
                        tension: 0.4,
                        pointRadius: 0,
                        borderWidth: 1,
                        yAxisID: 'y'
                    },
                    {
                        label: 'VReg',
                        data: [],
                        borderColor: '#2DA8B7',
                        fill: false,
                        tension: 0.4,
                        pointRadius: 0,
                        borderWidth: 1.5,
                        yAxisID: 'y1'
                    },
                    {
                        label: 'ASIC',
                        data: [],
                        borderColor: '#C84847',
                        fill: false,
                        tension: 0.4,
                        pointRadius: 0,
                        borderWidth: 1.5,
                        yAxisID: 'y1'
                    }
                ]
            },
            options: {
                responsive: true,
                maintainAspectRatio: false,
                animation: false,
                interaction: {
                    mode: 'index',
                    intersect: false
                },
                plugins: { legend: { display: false } },
                scales: {
                    x: {
                        type: 'time',
                        time: {
                            unit: 'minute',
                            stepSize: 15,
                            displayFormats: { minute: 'HH:mm' }
                        },
                        grid: { color: 'rgba(255,255,255,0.1)' },
                        ticks: {
                            color: '#8892a0',
                            maxTicksLimit: 5
                        }
                    },
                    y: {
                        type: 'linear',
                        position: 'left',
                        grid: { color: 'rgba(255,255,255,0.1)' },
                        ticks: {
                            color: '#a564f6',
                            callback: (v) => (v / 1000).toFixed(2) + ' TH/s'
                        }
                    },
                    y1: {
                        type: 'linear',
                        position: 'right',
                        grid: { drawOnChartArea: false },
                        ticks: {
                            color: '#C84847',
                            callback: (v) => v.toFixed(0) + ' °C'
                        },
                        min: 30,
                        max: 60
                    }
                }
            }
        });

        this.loadHashrateHistory();
    }

    async loadHashrateHistory() {
        try {
            const response = await fetch('/api/history');
            if (!response.ok) return;

            const history = await response.json();

            if (this.hashrateChart && history.length > 0) {
                // Get hashrate values and filter outliers
                const hashrates = history.map(h => h.hashrate || 0).filter(v => v > 0);
                const hashrates10m = history.map(h => h.hashrate10m || 0);
                const hashrates1h = history.map(h => h.hashrate1h || 0);

                // Calculate median to detect outliers
                const sorted = [...hashrates].sort((a, b) => a - b);
                const median = sorted[Math.floor(sorted.length / 2)] || 0;
                const threshold = median * 0.7; // Values below 70% of median are outliers

                // Filter history to remove outliers
                const filtered = history.filter(h => (h.hashrate || 0) >= threshold);

                if (filtered.length > 0) {
                    const labels = filtered.map(h => new Date(h.timestamp));
                    const hr = filtered.map(h => h.hashrate || 0);
                    const hr10m = filtered.map(h => h.hashrate10m || 0);
                    const hr1h = filtered.map(h => h.hashrate1h || 0);
                    const tempVreg = filtered.map(h => h.tempVreg || 0);
                    const tempAsic = filtered.map(h => h.tempAsic || 0);

                    // Calculate Y-axis range based on filtered data (tight range like ESP-Miner)
                    const allHashrates = [...hr, ...hr10m.filter(v => v > 0), ...hr1h.filter(v => v > 0)];
                    const minHash = Math.min(...allHashrates);
                    const maxHash = Math.max(...allHashrates);
                    const range = maxHash - minHash;
                    const padding = range * 0.15; // 15% padding

                    // Update Y-axis to show tight range
                    this.hashrateChart.options.scales.y.min = Math.max(0, minHash - padding);
                    this.hashrateChart.options.scales.y.max = maxHash + padding;

                    // Calculate temperature Y-axis range (base: 30-60, expands if needed)
                    const allTemps = [...tempVreg, ...tempAsic].filter(v => v > 0);
                    if (allTemps.length > 0) {
                        const minTemp = Math.min(...allTemps);
                        const maxTemp = Math.max(...allTemps);
                        // Use 30-60 as default, but expand if data exceeds
                        this.hashrateChart.options.scales.y1.min = Math.min(30, Math.floor(minTemp - 5));
                        this.hashrateChart.options.scales.y1.max = Math.max(60, Math.ceil(maxTemp + 5));
                    }

                    this.hashrateChart.data.labels = labels;
                    this.hashrateChart.data.datasets[0].data = hr;
                    this.hashrateChart.data.datasets[1].data = filtered.map(h => h.hashrate10m || 0);
                    this.hashrateChart.data.datasets[2].data = filtered.map(h => h.hashrate1h || 0);
                    this.hashrateChart.data.datasets[3].data = tempVreg;
                    this.hashrateChart.data.datasets[4].data = tempAsic;
                    this.hashrateChart.update('none');
                }
            }
        } catch (error) {
            console.error('Error loading hashrate history:', error);
        }
    }

    refreshHashrateChart() {
        if (this.currentPage === 'dashboard' && this.hashrateChart) {
            this.loadHashrateHistory();
        }
    }

    connectWebSocket() {
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const wsUrl = protocol + '//' + window.location.host + '/api/ws';

        try {
            this.ws = new WebSocket(wsUrl);

            this.ws.onopen = () => {
                console.log('WebSocket connected');
                this.wsReconnectDelay = 1000;
            };

            this.ws.onmessage = (event) => {
                try {
                    const message = JSON.parse(event.data);
                    this.handleWebSocketMessage(message);
                } catch (error) {
                    console.error('Error parsing WebSocket message:', error);
                }
            };

            this.ws.onclose = () => {
                console.log('WebSocket disconnected, reconnecting...');
                setTimeout(() => {
                    this.wsReconnectDelay = Math.min(this.wsReconnectDelay * 2, 30000);
                    this.connectWebSocket();
                }, this.wsReconnectDelay);
            };

            this.ws.onerror = (error) => console.error('WebSocket error:', error);
        } catch (error) {
            console.error('Error connecting WebSocket:', error);
            setTimeout(() => this.connectWebSocket(), this.wsReconnectDelay);
        }
    }

    handleWebSocketMessage(message) {
        switch (message.type) {
            case 'share':
                this.addShare(message.data);
                break;
            case 'snapshot':
                this.updateMinerSnapshot(message.data);
                break;
            case 'stats':
                this.updateSummaryCards(message.data);
                break;
            case 'block':
                this.handleBlockFound(message.data);
                break;
        }
    }

    handleBlockFound(block) {
        console.log('BLOCK FOUND!', block);
        this.showBlockNotification(block);

        // Increment block counter on dashboard
        const blocksEl = document.getElementById('total-blocks');
        if (blocksEl) {
            const currentCount = parseInt(blocksEl.textContent) || 0;
            blocksEl.textContent = currentCount + 1;
        }

        // Refresh all related data
        this.loadEarnings();
        this.fetchMoneyMakers();
        this.fetchCompetition();
    }

    showBlockNotification(block) {
        // Create full-screen celebration overlay
        const overlay = document.createElement('div');
        overlay.className = 'block-found-overlay';
        overlay.innerHTML = `
            <div class="block-found-content">
                <div class="block-icon">&#x1F389;</div>
                <h1>BLOCK FOUND!</h1>
                <div class="block-details">
                    <div class="block-miner">${block.hostname || block.minerIp}</div>
                    <div class="block-diff">Difficulty: ${this.formatDifficulty(block.difficulty)}</div>
                    <div class="block-network">Network: ${this.formatDifficulty(block.networkDifficulty)}</div>
                    <div class="block-time">${new Date(block.timestamp).toLocaleTimeString()}</div>
                </div>
            </div>
        `;
        document.body.appendChild(overlay);

        // Add confetti animation
        this.createConfetti(overlay);

        // Auto-remove after 10 seconds or on click
        const removeOverlay = () => {
            overlay.classList.add('fade-out');
            setTimeout(() => overlay.remove(), 500);
        };
        overlay.addEventListener('click', removeOverlay);
        setTimeout(removeOverlay, 10000);
    }

    createConfetti(container) {
        const colors = ['#FFD700', '#FF6B6B', '#4ECDC4', '#45B7D1', '#96CEB4', '#FFEAA7'];
        for (let i = 0; i < 100; i++) {
            const confetti = document.createElement('div');
            confetti.className = 'confetti';
            confetti.style.left = Math.random() * 100 + '%';
            confetti.style.backgroundColor = colors[Math.floor(Math.random() * colors.length)];
            confetti.style.animationDelay = Math.random() * 2 + 's';
            confetti.style.animationDuration = (Math.random() * 2 + 2) + 's';
            container.appendChild(confetti);
        }
    }

    addShare(share) {
        this.shares.unshift(share);
        if (this.shares.length > this.maxShares) this.shares.pop();
        this.renderShares();

        // Also update shares history for the Shares page
        this.sharesHistory.unshift(share);

        // Keep shares from last 15 minutes (to cover 10-min chart window)
        const cutoffTime = new Date(Date.now() - 15 * 60 * 1000);
        this.sharesHistory = this.sharesHistory.filter(s => new Date(s.timestamp) >= cutoffTime);
        if (this.currentPage === 'shares') {
            this.renderSharesHistoryFeed();
            this.updateSharesCharts();
        }

        // Update competition in real-time (debounced to avoid too many requests)
        this.scheduleCompetitionRefresh();
    }

    scheduleCompetitionRefresh() {
        // Debounce: wait 500ms after last share before refreshing
        if (this.competitionRefreshTimeout) {
            clearTimeout(this.competitionRefreshTimeout);
        }
        this.competitionRefreshTimeout = setTimeout(() => {
            this.fetchCompetition();
        }, 500);
    }

    renderShares() {
        const feed = document.getElementById('shares-feed');
        if (!feed) return;

        feed.textContent = '';

        if (this.shares.length === 0) {
            const emptyState = document.createElement('div');
            emptyState.className = 'empty-state';
            emptyState.textContent = 'Waiting for shares...';
            feed.appendChild(emptyState);
            return;
        }

        const maxDiff = Math.max(...this.shares.map(s => s.difficulty || 0));
        this.shares.forEach(share => feed.appendChild(this.createShareItem(share, maxDiff)));
    }

    createShareItem(share, maxDiff) {
        const item = document.createElement('div');
        item.className = 'share-item';

        const time = document.createElement('span');
        time.className = 'share-time';
        time.textContent = this.formatTime(share.timestamp);

        const miner = document.createElement('span');
        miner.className = 'share-miner';
        miner.textContent = share.hostname || share.minerIp || share.ip || 'Unknown';

        const diffContainer = document.createElement('div');
        diffContainer.className = 'share-diff';

        const diffBar = document.createElement('div');
        diffBar.className = 'diff-bar';
        const barWidth = maxDiff > 0 ? ((share.difficulty || 0) / maxDiff) * 100 : 0;
        diffBar.style.width = Math.max(barWidth, 5) + '%';

        const diffValue = document.createElement('span');
        diffValue.className = 'diff-value';
        diffValue.textContent = this.formatDifficulty(share.difficulty || 0);

        diffContainer.appendChild(diffBar);
        diffContainer.appendChild(diffValue);

        const status = document.createElement('span');
        status.className = 'share-status accepted';
        status.textContent = '\u2713';

        item.appendChild(time);
        item.appendChild(miner);
        item.appendChild(diffContainer);
        item.appendChild(status);

        return item;
    }

    updateMinerSnapshot(snapshot) {
        const ip = snapshot.minerIp || snapshot.ip;
        const minerCard = document.querySelector('.miner-card[data-ip="' + ip + '"]');
        if (!minerCard) return;

        const hashrateEl = minerCard.querySelector('.stat-item:nth-child(1) .stat-value');
        if (hashrateEl) hashrateEl.textContent = this.formatHashrate((snapshot.hashRate || 0) * 1e9);

        const powerEl = minerCard.querySelector('.stat-item:nth-child(2) .stat-value');
        if (powerEl) powerEl.textContent = this.formatPower(snapshot.power || 0);

        const tempEl = minerCard.querySelector('.stat-item:nth-child(3) .stat-value');
        if (tempEl) tempEl.textContent = this.formatTemp(snapshot.temperature || 0);

        const statusDot = minerCard.querySelector('.status-dot');
        const statusText = minerCard.querySelector('.miner-status span:last-child');
        const isOnline = snapshot.poolConnected !== false;

        if (isOnline) {
            minerCard.classList.remove('offline');
            if (statusDot) statusDot.classList.remove('offline');
            if (statusText) statusText.textContent = 'ONLINE';
        } else {
            minerCard.classList.add('offline');
            if (statusDot) statusDot.classList.add('offline');
            if (statusText) statusText.textContent = 'OFFLINE';
        }
    }

    openScanModal() {
        const modal = document.getElementById('scan-modal');
        if (modal) modal.classList.remove('hidden');

        const status = document.getElementById('scan-status');
        if (status) {
            status.textContent = 'Click scan to discover miners on your network';
            status.classList.remove('scanning');
        }

        const results = document.getElementById('scan-results');
        if (results) results.textContent = '';
    }

    closeScanModal() {
        const modal = document.getElementById('scan-modal');
        if (modal) modal.classList.add('hidden');
    }

    async scan() {
        const status = document.getElementById('scan-status');
        const results = document.getElementById('scan-results');
        const startBtn = document.getElementById('start-scan-btn');

        if (status) {
            status.classList.add('scanning');
            status.textContent = '';
            const spinner = document.createElement('span');
            spinner.className = 'spinner';
            status.appendChild(spinner);
            status.appendChild(document.createTextNode(' Scanning network...'));
        }
        if (results) results.textContent = '';
        if (startBtn) startBtn.disabled = true;

        try {
            const response = await fetch('/api/scan', { method: 'POST' });
            if (!response.ok) throw new Error('Scan failed');

            const data = await response.json();
            this.renderScanResults(data.results || []);

            if (status) {
                status.classList.remove('scanning');
                status.textContent = 'Found ' + (data.results || []).length + ' miners';
            }
        } catch (error) {
            console.error('Scan error:', error);
            if (status) {
                status.classList.remove('scanning');
                status.textContent = 'Scan failed: ' + error.message;
            }
        } finally {
            if (startBtn) startBtn.disabled = false;
        }
    }

    renderScanResults(miners) {
        const results = document.getElementById('scan-results');
        if (!results) return;

        results.textContent = '';

        if (miners.length === 0) {
            const empty = document.createElement('div');
            empty.className = 'empty-state';
            empty.textContent = 'No miners found on the network.';
            results.appendChild(empty);
            return;
        }

        miners.forEach(miner => {
            const item = document.createElement('div');
            item.className = 'scan-result-item';

            const info = document.createElement('div');
            info.className = 'scan-result-info';

            const ip = document.createElement('span');
            ip.className = 'scan-result-ip';
            ip.textContent = miner.ip;

            const model = document.createElement('span');
            model.className = 'scan-result-model';
            model.textContent = miner.deviceModel || miner.hostname || 'Unknown model';

            info.appendChild(ip);
            info.appendChild(model);

            const addBtn = document.createElement('button');
            addBtn.className = 'btn btn-add';
            addBtn.textContent = '+ Add';
            addBtn.addEventListener('click', () => this.addMiner(miner.ip));

            item.appendChild(info);
            item.appendChild(addBtn);
            results.appendChild(item);
        });
    }

    async addMiner(ip) {
        try {
            const response = await fetch('/api/miners', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ ip: ip })
            });

            if (!response.ok) throw new Error('Failed to add miner');

            await this.fetchMiners();
            await this.fetchStats();

            const results = document.getElementById('scan-results');
            if (results) {
                results.querySelectorAll('.scan-result-item').forEach(item => {
                    const itemIp = item.querySelector('.scan-result-ip');
                    if (itemIp && itemIp.textContent === ip) {
                        const btn = item.querySelector('.btn-add');
                        if (btn) {
                            btn.textContent = 'Added';
                            btn.disabled = true;
                        }
                    }
                });
            }
        } catch (error) {
            console.error('Error adding miner:', error);
            this.showToast('Failed to add miner: ' + error.message, 'error');
        }
    }

    async addManualMiner() {
        const input = document.getElementById('manual-ip-input');
        const btn = document.getElementById('manual-add-btn');
        if (!input) return;

        const ip = input.value.trim();
        if (!ip) {
            this.showToast('Enter an IP address', 'error');
            return;
        }

        btn.disabled = true;
        btn.textContent = 'Adding...';

        try {
            const response = await fetch('/api/miners', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ ip: ip })
            });

            if (!response.ok) {
                const text = await response.text();
                throw new Error(text || 'Failed to add miner');
            }

            const miner = await response.json();
            this.showToast('Added ' + (miner.hostname || ip), 'success');
            input.value = '';

            await this.fetchMiners();
            await this.fetchStats();
        } catch (error) {
            console.error('Error adding miner:', error);
            this.showToast('Failed: ' + error.message, 'error');
        } finally {
            btn.disabled = false;
            btn.textContent = 'Add';
        }
    }

    async loadSharesPage() {
        await Promise.all([this.loadBestShares(), this.loadSharesHistory()]);

        // Always reinitialize charts when entering the page
        // This ensures they render correctly after being hidden
        this.initSharesCharts();
        this.renderSharesHistoryFeed();

        // Start periodic refresh for sliding window effect (fast updates, no animation)
        if (this.sharesChartInterval) clearInterval(this.sharesChartInterval);
        this.sharesChartInterval = setInterval(() => {
            if (this.currentPage === 'shares') {
                this.updateSharesCharts();
            }
        }, 500);
    }

    async loadBestShares() {
        try {
            const response = await fetch('/api/shares/best');
            if (!response.ok) return;

            const data = await response.json();

            const alltimeEl = document.getElementById('best-share-alltime');
            const alltimeInfo = document.getElementById('best-share-alltime-info');
            const sessionEl = document.getElementById('best-share-session');
            const sessionInfo = document.getElementById('best-share-session-info');

            if (data.allTime && alltimeEl) {
                alltimeEl.textContent = this.formatDifficulty(data.allTime.difficulty);
                alltimeInfo.textContent = data.allTime.hostname || data.allTime.minerIp;
            }

            if (data.session && sessionEl) {
                sessionEl.textContent = this.formatDifficulty(data.session.difficulty);
                sessionInfo.textContent = data.session.hostname || data.session.minerIp;
            }
        } catch (error) {
            console.error('Error loading best shares:', error);
        }
    }

    async loadSharesHistory() {
        try {
            const response = await fetch('/api/shares?hours=1&limit=5000');
            if (!response.ok) return;

            const newShares = await response.json();

            // Merge with existing shares instead of replacing
            // This preserves shares received via WebSocket
            if (this.sharesHistory.length === 0) {
                this.sharesHistory = newShares;
            } else {
                // Add any shares from API that we don't already have
                const existingIds = new Set(this.sharesHistory.map(s => s.id));
                const uniqueNewShares = newShares.filter(s => !existingIds.has(s.id));
                this.sharesHistory = [...this.sharesHistory, ...uniqueNewShares];

                // Sort by timestamp descending and keep last 15 minutes
                this.sharesHistory.sort((a, b) => new Date(b.timestamp) - new Date(a.timestamp));
                const cutoffTime = new Date(Date.now() - 15 * 60 * 1000);
                this.sharesHistory = this.sharesHistory.filter(s => new Date(s.timestamp) >= cutoffTime);
            }
        } catch (error) {
            console.error('Error loading shares history:', error);
        }
    }

    renderSharesHistoryFeed() {
        const feed = document.getElementById('shares-history-feed');
        if (!feed) return;

        feed.textContent = '';

        const filteredShares = this.getFilteredShares();

        if (filteredShares.length === 0) {
            const empty = document.createElement('div');
            empty.className = 'empty-state';
            empty.textContent = 'No shares recorded';
            feed.appendChild(empty);
            return;
        }

        const displayedShares = filteredShares.slice(0, 10);
        const maxDiff = Math.max(...displayedShares.map(s => s.difficulty || 0));
        displayedShares.forEach(share => feed.appendChild(this.createShareItem(share, maxDiff)));
    }

    initSharesCharts() {
        this.initSharesScatterChart();
        this.initSharesHistogramChart();
        this.bindSharesFilterEvent();
    }

    bindSharesFilterEvent() {
        const filter = document.getElementById('shares-miner-filter');
        if (!filter) return;

        filter.onchange = () => {
            this.updateSharesCharts();
            this.renderSharesHistoryFeed();
        };
    }

    getFilteredShares() {
        const filter = document.getElementById('shares-miner-filter');
        const selectedMiner = filter ? filter.value : '';

        if (!selectedMiner) {
            return this.sharesHistory;
        }

        return this.sharesHistory.filter(s => {
            const minerIp = s.minerIp || s.ip || '';
            return minerIp === selectedMiner;
        });
    }

    updateSharesCharts() {
        const filteredShares = this.getFilteredShares();

        // Update scatter chart with sliding window (last 10 minutes for better visibility)
        if (this.sharesScatterChart) {
            const now = new Date();
            const windowMinutes = 10;
            const windowStart = new Date(now.getTime() - windowMinutes * 60 * 1000);

            const data = filteredShares
                .filter(s => new Date(s.timestamp) >= windowStart)
                .map(s => ({ x: new Date(s.timestamp), y: s.difficulty || 0 }));

            this.sharesScatterChart.data.datasets[0].data = data;
            this.sharesScatterChart.options.scales.x.min = windowStart;
            this.sharesScatterChart.options.scales.x.max = now;
            this.sharesScatterChart.update('none');
        }

        // Update histogram chart
        if (this.sharesHistogramChart) {
            const buckets = { '<1K': 0, '1K-10K': 0, '10K-100K': 0, '100K-1M': 0, '1M-100M': 0, '100M-1G': 0, '>1G': 0 };

            filteredShares.forEach(s => {
                const d = s.difficulty || 0;
                if (d < 1000) buckets['<1K']++;
                else if (d < 10000) buckets['1K-10K']++;
                else if (d < 100000) buckets['10K-100K']++;
                else if (d < 1000000) buckets['100K-1M']++;
                else if (d < 100000000) buckets['1M-100M']++;
                else if (d < 1000000000) buckets['100M-1G']++;
                else buckets['>1G']++;
            });

            this.sharesHistogramChart.data.datasets[0].data = Object.values(buckets);
            this.sharesHistogramChart.update('none');
        }
    }

    // Format axis tick value to K, M, G, T
    formatAxisDifficulty(v) {
        if (v >= 1e12) return (v / 1e12).toFixed(0) + 'T';
        if (v >= 1e9) return (v / 1e9).toFixed(0) + 'G';
        if (v >= 1e6) return (v / 1e6).toFixed(0) + 'M';
        if (v >= 1e3) return (v / 1e3).toFixed(0) + 'K';
        return '';
    }

    initSharesScatterChart() {
        const ctx = document.getElementById('shares-scatter-chart');
        if (!ctx) return;

        if (this.sharesScatterChart) this.sharesScatterChart.destroy();

        // Show last 10 minutes of shares with sliding window
        const now = new Date();
        const windowMinutes = 10;
        const windowStart = new Date(now.getTime() - windowMinutes * 60 * 1000);

        const filteredShares = this.getFilteredShares();
        const data = filteredShares
            .filter(s => new Date(s.timestamp) >= windowStart)
            .map(s => ({ x: new Date(s.timestamp), y: s.difficulty || 0 }));

        this.sharesScatterChart = new Chart(ctx, {
            type: 'scatter',
            data: {
                datasets: [{
                    label: 'Share Difficulty',
                    data: data,
                    backgroundColor: 'rgba(0, 212, 255, 0.6)',
                    borderColor: '#00d4ff',
                    pointRadius: 4
                }]
            },
            options: {
                responsive: true,
                maintainAspectRatio: false,
                animation: false,
                plugins: { legend: { display: false } },
                scales: {
                    x: {
                        type: 'time',
                        time: {
                            unit: 'minute',
                            stepSize: 1,
                            displayFormats: { minute: 'HH:mm:ss' }
                        },
                        min: windowStart,
                        max: now,
                        grid: { color: 'rgba(255,255,255,0.1)' },
                        ticks: { color: '#8892a0', maxTicksLimit: 7 }
                    },
                    y: {
                        type: 'logarithmic',
                        min: 1000,
                        grid: { color: 'rgba(255,255,255,0.1)' },
                        ticks: {
                            color: '#8892a0',
                            autoSkip: false,
                            callback: (v) => {
                                // Only show powers of 10
                                const log = Math.log10(v);
                                if (Math.abs(log - Math.round(log)) < 0.01) {
                                    return this.formatAxisDifficulty(v);
                                }
                                return '';
                            }
                        }
                    }
                }
            }
        });
    }

    initSharesHistogramChart() {
        const ctx = document.getElementById('shares-histogram-chart');
        if (!ctx) return;

        if (this.sharesHistogramChart) this.sharesHistogramChart.destroy();

        const buckets = { '<1K': 0, '1K-10K': 0, '10K-100K': 0, '100K-1M': 0, '1M-100M': 0, '100M-1G': 0, '>1G': 0 };

        this.sharesHistory.forEach(s => {
            const d = s.difficulty || 0;
            if (d < 1000) buckets['<1K']++;
            else if (d < 10000) buckets['1K-10K']++;
            else if (d < 100000) buckets['10K-100K']++;
            else if (d < 1000000) buckets['100K-1M']++;
            else if (d < 100000000) buckets['1M-100M']++;
            else if (d < 1000000000) buckets['100M-1G']++;
            else buckets['>1G']++;
        });

        this.sharesHistogramChart = new Chart(ctx, {
            type: 'bar',
            data: {
                labels: Object.keys(buckets),
                datasets: [{
                    label: 'Share Count',
                    data: Object.values(buckets),
                    backgroundColor: [
                        'rgba(0, 212, 255, 0.7)',
                        'rgba(0, 255, 136, 0.7)',
                        'rgba(255, 170, 0, 0.7)',
                        'rgba(255, 0, 255, 0.7)',
                        'rgba(255, 102, 0, 0.7)',
                        'rgba(255, 68, 68, 0.7)',
                        'rgba(255, 215, 0, 0.7)'
                    ],
                    borderColor: '#00d4ff',
                    borderWidth: 1
                }]
            },
            options: {
                responsive: true,
                maintainAspectRatio: false,
                plugins: { legend: { display: false } },
                scales: {
                    x: { grid: { color: 'rgba(255,255,255,0.1)' }, ticks: { color: '#8892a0' } },
                    y: { grid: { color: 'rgba(255,255,255,0.1)' }, ticks: { color: '#8892a0' } }
                }
            }
        });
    }

    updateMinerFilter() {
        const filter = document.getElementById('shares-miner-filter');
        if (!filter) return;

        // Preserve current selection
        const currentValue = filter.value;

        while (filter.options.length > 1) filter.remove(1);

        this.miners.forEach(m => {
            const opt = document.createElement('option');
            opt.value = m.ip;
            opt.textContent = m.hostname || m.ip;
            filter.appendChild(opt);
        });

        // Restore selection if it still exists
        if (currentValue) {
            filter.value = currentValue;
        }
    }

    async fetchSettings() {
        try {
            const response = await fetch('/api/settings');
            if (response.ok) this.settings = await response.json();
        } catch (error) {
            console.error('Error fetching settings:', error);
        }
    }

    async loadSettingsPage() {
        await this.fetchSettings();
        await this.loadDBSize();
        this.populateSettingsForm();
        this.renderSettingsMinersList();
    }

    populateSettingsForm() {
        const s = this.settings;
        if (s.alerts) {
            this.setInputValue('alert-webhook', s.alerts.webhook_url || '');
            this.setInputValue('alert-offline', s.alerts.offline_minutes || 5);
            this.setInputValue('alert-temp', s.alerts.temp_threshold_c || 70);
            this.setInputValue('alert-hashrate', 100 - (s.alerts.hashrate_drop_pct || 20));
            this.setInputValue('alert-fan', s.alerts.fan_rpm_below || 1000);
            this.setInputValue('alert-wifi', s.alerts.wifi_signal_below || -70);
            this.setCheckboxValue('alert-rejected', s.alerts.on_share_rejected);
            this.setCheckboxValue('alert-pool-disconnect', s.alerts.on_pool_disconnected);
            this.setCheckboxValue('alert-best-diff', s.alerts.on_new_best_diff);
            this.setCheckboxValue('alert-block-found', s.alerts.on_block_found);
            this.setCheckboxValue('alert-new-leader', s.alerts.on_new_leader);
        }
        if (s.energy) {
            this.setInputValue('energy-cost', s.energy.cost_per_kwh || 0.12);
            this.setSelectValue('energy-currency', s.energy.currency || 'USD');
        }
        if (s.retention) {
            this.setInputValue('retention-days', s.retention.metrics_retention_days || 30);
        }
    }

    setInputValue(id, value) {
        const el = document.getElementById(id);
        if (el) el.value = value;
    }

    setCheckboxValue(id, value) {
        const el = document.getElementById(id);
        if (el) el.checked = !!value;
    }

    setSelectValue(id, value) {
        const el = document.getElementById(id);
        if (el) el.value = value;
    }

    renderSettingsMinersList() {
        const list = document.getElementById('settings-miners-list');
        if (!list) return;

        list.textContent = '';

        if (this.miners.length === 0) {
            const empty = document.createElement('div');
            empty.className = 'empty-state';
            empty.textContent = 'No miners configured';
            list.appendChild(empty);
            return;
        }

        this.miners.forEach(m => {
            const item = document.createElement('div');
            item.className = 'settings-miner-item';

            const info = document.createElement('div');
            info.className = 'settings-miner-info';

            const name = document.createElement('span');
            name.className = 'settings-miner-name';
            name.textContent = m.hostname || m.ip;

            const ip = document.createElement('span');
            ip.className = 'settings-miner-ip';
            ip.textContent = m.ip;

            info.appendChild(name);
            info.appendChild(ip);

            const removeBtn = document.createElement('button');
            removeBtn.className = 'btn btn-remove';
            removeBtn.textContent = 'Remove';
            removeBtn.addEventListener('click', () => this.removeMiner(m.ip));

            item.appendChild(info);
            item.appendChild(removeBtn);
            list.appendChild(item);
        });
    }

    async removeMiner(ip) {
        if (!confirm('Remove miner ' + ip + '?')) return;

        try {
            const response = await fetch('/api/miners/' + ip, { method: 'DELETE' });
            if (!response.ok) throw new Error('Failed to remove miner');

            await this.fetchMiners();
            this.renderSettingsMinersList();
        } catch (error) {
            console.error('Error removing miner:', error);
            this.showToast('Failed to remove miner', 'error');
        }
    }

    async loadDBSize() {
        try {
            const response = await fetch('/api/dbsize');
            if (response.ok) {
                const data = await response.json();
                const el = document.getElementById('db-size');
                if (el) el.textContent = data.sizeHuman || 'Unknown';
            }
        } catch (error) {
            console.error('Error loading DB size:', error);
        }
    }

    async loadCoins() {
        try {
            const response = await fetch('/api/coins');
            if (response.ok) {
                this.coins = await response.json();
            }
        } catch (error) {
            console.error('Error loading coins:', error);
        }
    }

    async loadEarnings() {
        try {
            const response = await fetch('/api/earnings');
            if (!response.ok) return;

            const data = await response.json();
            const coins = data.coins || [];

            // Store for carousel
            this.earningsCoins = coins;
            this.earningsTotalCurrentUsd = data.totalCurrentUsd || 0;

            // If no blocks mined yet, show empty state
            if (coins.length === 0) {
                const totalCoinsEl = document.getElementById('earnings-total-coins');
                if (totalCoinsEl) totalCoinsEl.textContent = '0';

                const rewardEl = document.getElementById('earnings-block-reward');
                if (rewardEl) rewardEl.textContent = '0';

                const blocksEl = document.getElementById('earnings-blocks-found');
                if (blocksEl) blocksEl.textContent = '0';

                const totalUsdEl = document.getElementById('earnings-total-usd');
                if (totalUsdEl) totalUsdEl.textContent = '≈ $0.00';

                const iconEl = document.getElementById('earnings-coin-icon');
                if (iconEl) iconEl.style.display = 'none';

                const dotsEl = document.getElementById('earnings-dots');
                if (dotsEl) dotsEl.textContent = '';

                // Stop carousel if running
                if (this.earningsCycleInterval) {
                    clearInterval(this.earningsCycleInterval);
                    this.earningsCycleInterval = null;
                }
                return;
            }

            // Clamp index if coins changed
            if (this.earningsCoinIndex >= coins.length) {
                this.earningsCoinIndex = 0;
            }

            // Render current coin (no animation on data refresh)
            this.renderEarningsCoin(this.earningsCoinIndex, false);

            // Render dots
            this.renderEarningsDots();

            // Start carousel if multiple coins and not already running
            if (coins.length > 1 && !this.earningsCycleInterval) {
                this.earningsCycleInterval = setInterval(() => {
                    this.earningsCoinIndex = (this.earningsCoinIndex + 1) % this.earningsCoins.length;
                    this.renderEarningsCoin(this.earningsCoinIndex, true);
                    this.renderEarningsDots();
                }, 5000);
            }

            // Stop carousel if only 1 coin left
            if (coins.length <= 1 && this.earningsCycleInterval) {
                clearInterval(this.earningsCycleInterval);
                this.earningsCycleInterval = null;
            }
        } catch (error) {
            console.error('Error loading earnings:', error);
        }
    }

    renderEarningsCoin(index, animate) {
        const coin = this.earningsCoins[index];
        if (!coin) return;

        const totalCoinsEl = document.getElementById('earnings-total-coins');
        const rewardEl = document.getElementById('earnings-block-reward');
        const blocksEl = document.getElementById('earnings-blocks-found');
        const totalUsdEl = document.getElementById('earnings-total-usd');
        const iconEl = document.getElementById('earnings-coin-icon');

        const updateContent = () => {
            // Icon
            if (iconEl && coin.coinIcon) {
                iconEl.src = coin.coinIcon;
                iconEl.alt = coin.coinSymbol || '';
                iconEl.style.display = 'inline-block';
            }

            // Total coins for this coin
            if (totalCoinsEl) {
                totalCoinsEl.textContent = this.formatCoins(coin.totalCoins) + ' ' + coin.coinSymbol;
            }

            // Avg reward per block
            if (rewardEl) {
                const avgReward = coin.blockCount > 0 ? coin.totalCoins / coin.blockCount : 0;
                rewardEl.textContent = this.formatCoins(avgReward) + ' ' + coin.coinSymbol;
            }

            // Block count for this coin
            if (blocksEl) {
                blocksEl.textContent = coin.blockCount;
            }

            // USD for this specific coin
            if (totalUsdEl) {
                totalUsdEl.textContent = '≈ $' + this.formatUSD(coin.currentUsd || 0);
            }
        };

        if (animate) {
            // Fade out
            const fadeTargets = [totalCoinsEl, rewardEl, blocksEl, totalUsdEl, iconEl].filter(Boolean);
            fadeTargets.forEach(el => el.style.opacity = '0');

            setTimeout(() => {
                updateContent();
                fadeTargets.forEach(el => el.style.opacity = '1');
            }, 200);
        } else {
            updateContent();
        }
    }

    renderEarningsDots() {
        const dotsEl = document.getElementById('earnings-dots');
        if (!dotsEl) return;

        // Only show dots if multiple coins
        if (this.earningsCoins.length <= 1) {
            dotsEl.textContent = '';
            return;
        }

        // Build dots using safe DOM methods
        dotsEl.textContent = '';
        for (let i = 0; i < this.earningsCoins.length; i++) {
            const dot = document.createElement('span');
            dot.className = 'earnings-dot' + (i === this.earningsCoinIndex ? ' active' : '');
            dotsEl.appendChild(dot);
        }
    }

    formatCoins(amount) {
        if (amount >= 1000000) return (amount / 1000000).toFixed(2) + 'M';
        if (amount >= 1000) return amount.toLocaleString(undefined, { minimumFractionDigits: 0, maximumFractionDigits: 2 });
        if (amount >= 1) return amount.toFixed(2);
        return amount.toFixed(4);
    }

    formatUSD(amount) {
        if (amount >= 1000000) return (amount / 1000000).toFixed(2) + 'M';
        if (amount >= 1000) return amount.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 });
        if (amount >= 1) return amount.toFixed(2);
        return amount.toFixed(4);
    }

    async saveSettings() {
        const newSettings = {
            alerts: {
                enabled: true,
                webhook_url: document.getElementById('alert-webhook')?.value || '',
                offline_minutes: parseInt(document.getElementById('alert-offline')?.value) || 5,
                temp_threshold_c: parseFloat(document.getElementById('alert-temp')?.value) || 70,
                hashrate_drop_pct: 100 - (parseInt(document.getElementById('alert-hashrate')?.value) || 80),
                fan_rpm_below: parseInt(document.getElementById('alert-fan')?.value) || 1000,
                wifi_signal_below: parseInt(document.getElementById('alert-wifi')?.value) || -70,
                on_share_rejected: document.getElementById('alert-rejected')?.checked || false,
                on_pool_disconnected: document.getElementById('alert-pool-disconnect')?.checked || false,
                on_new_best_diff: document.getElementById('alert-best-diff')?.checked || false,
                on_block_found: document.getElementById('alert-block-found')?.checked || false,
                on_new_leader: document.getElementById('alert-new-leader')?.checked || false
            },
            energy: {
                cost_per_kwh: parseFloat(document.getElementById('energy-cost')?.value) || 0.12,
                currency: document.getElementById('energy-currency')?.value || 'USD'
            },
            retention: {
                metrics_retention_days: parseInt(document.getElementById('retention-days')?.value) || 30
            }
        };

        try {
            const response = await fetch('/api/settings', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(newSettings)
            });

            if (!response.ok) throw new Error('Failed to save settings');

            this.showToast('Settings saved successfully');
            await this.fetchSettings();
        } catch (error) {
            console.error('Error saving settings:', error);
            this.showToast('Failed to save settings', 'error');
        }
    }

    async purgeData() {
        const days = parseInt(document.getElementById('retention-days')?.value) || 30;
        if (!confirm('Purge all data older than ' + days + ' days?')) return;

        try {
            const response = await fetch('/api/purge?days=' + days, { method: 'POST' });
            if (!response.ok) throw new Error('Failed to purge data');

            this.showToast('Data purged successfully');
            await this.loadDBSize();
        } catch (error) {
            console.error('Error purging data:', error);
            this.showToast('Failed to purge data', 'error');
        }
    }

    formatHashrate(hashrate) {
        if (hashrate >= 1e15) return (hashrate / 1e15).toFixed(2) + ' PH/s';
        if (hashrate >= 1e12) return (hashrate / 1e12).toFixed(2) + ' TH/s';
        if (hashrate >= 1e9) return (hashrate / 1e9).toFixed(2) + ' GH/s';
        if (hashrate >= 1e6) return (hashrate / 1e6).toFixed(2) + ' MH/s';
        if (hashrate >= 1e3) return (hashrate / 1e3).toFixed(2) + ' KH/s';
        return hashrate.toFixed(0) + ' H/s';
    }

    formatPower(power) {
        if (power >= 1000) return (power / 1000).toFixed(2) + ' kW';
        return power.toFixed(0) + ' W';
    }

    getCurrencySymbol(currency) {
        const symbols = {
            'USD': '$',
            'EUR': '€',
            'BRL': 'R$',
            'GBP': '£',
            'JPY': '¥'
        };
        return symbols[currency] || '$';
    }

    formatTemp(temp) {
        return temp.toFixed(0) + '\u00B0C';
    }

    formatDifficulty(diff) {
        if (diff >= 1e12) return (diff / 1e12).toFixed(2) + 'T';
        if (diff >= 1e9) return (diff / 1e9).toFixed(2) + 'G';
        if (diff >= 1e6) return (diff / 1e6).toFixed(2) + 'M';
        if (diff >= 1e3) return (diff / 1e3).toFixed(2) + 'K';
        return diff.toFixed(0);
    }

    formatTime(timestamp) {
        if (!timestamp) return '--:--:--';
        const date = new Date(timestamp);
        return date.toLocaleTimeString('en-US', { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' });
    }

    formatUptime(seconds) {
        const days = Math.floor(seconds / 86400);
        const hours = Math.floor((seconds % 86400) / 3600);
        const mins = Math.floor((seconds % 3600) / 60);
        if (days > 0) return days + 'd ' + hours + 'h';
        if (hours > 0) return hours + 'h ' + mins + 'm';
        return mins + 'm';
    }

    async testWebhook() {
        const btn = document.querySelector('.btn-test');
        if (btn) { btn.disabled = true; btn.textContent = 'Sending...'; }
        try {
            const response = await fetch('/api/alerts/test', { method: 'POST' });
            if (!response.ok) {
                const text = await response.text();
                throw new Error(text);
            }
            this.showToast('Test alert sent! Check your Discord channel.');
        } catch (error) {
            this.showToast('Webhook test failed: ' + error.message, 'error');
        } finally {
            if (btn) { btn.disabled = false; btn.textContent = 'Test'; }
        }
    }

    showToast(message, type = 'success') {
        const container = document.getElementById('toast-container');
        if (!container) return;
        const toast = document.createElement('div');
        toast.className = `toast toast-${type}`;
        toast.textContent = message;
        container.appendChild(toast);
        setTimeout(() => toast.remove(), 3300);
    }
}

const app = new MinerHQ();
window.app = app; // For debugging
