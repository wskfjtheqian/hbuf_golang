class StressTestUI {
    // 在构造函数中添加调试模式
    constructor() {
        this.ws = null;
        this.isRunning = false;
        this.charts = {};
        this.apiDistribution = {
            labels: [],
            data: [],
            counts: []
        };
        this.debugMode = true; // 设为false可关闭调试日志
        this.lastLogTime = 0;

        this.initApiList()
        this.initCharts();
        this.bindEvents();
        this.connectWebSocket();
        this.updateDetailedStats();
    }

    initCharts() {
        // 1. TPS图表
        const tpsCtx = document.getElementById('tps-chart').getContext('2d');
        this.charts.tps = new Chart(tpsCtx, {
            type: 'line',
            data: {
                datasets: [{
                    label: 'TPS',
                    data: [],
                    borderColor: '#3b82f6',
                    backgroundColor: 'rgba(59, 130, 246, 0.1)',
                    fill: true,
                    tension: 0.2,
                    borderWidth: 2,
                    pointRadius: 0,
                    pointHoverRadius: 4
                }]
            },
            options: {
                responsive: true,
                maintainAspectRatio: false,
                animation: {
                    duration: 0
                },
                plugins: {
                    legend: { display: false },
                    tooltip: {
                        mode: 'nearest',
                        intersect: false,
                        callbacks: {
                            label: (context) => `TPS: ${context.parsed.y.toFixed(2)}`
                        }
                    }
                },
                scales: {
                    x: {
                        type: 'time',
                        time: {
                            unit: 'second',
                            displayFormats: {
                                second: 'HH:mm:ss'
                            },
                            tooltipFormat: 'HH:mm:ss'
                        },
                        title: {
                            display: true,
                            text: '时间'
                        },
                        min: () => {
                            // 动态设置最小时间（当前时间-60秒）
                            return new Date(Date.now() - 60000);
                        },
                        max: () => {
                            // 动态设置最大时间（当前时间+5秒）
                            return new Date(Date.now() + 5000);
                        },
                        ticks: {
                            maxRotation: 0,
                            autoSkip: true,
                            maxTicksLimit: 10
                        }
                    },
                    y: {
                        beginAtZero: true,
                        title: {
                            display: true,
                            text: 'TPS'
                        },
                        ticks: {
                            precision: 0
                        }
                    }
                }
            }
        });

        // 2. 耗时图表
        const latencyCtx = document.getElementById('latency-chart').getContext('2d');
        this.charts.latency = new Chart(latencyCtx, {
            type: 'line',
            data: {
                datasets: [{
                    label: '平均耗时 (ms)',
                    data: [],
                    borderColor: '#8b5cf6',
                    backgroundColor: 'rgba(139, 92, 246, 0.1)',
                    fill: true,
                    tension: 0.2,
                    borderWidth: 2,
                    pointRadius: 0
                }]
            },
            options: {
                responsive: true,
                maintainAspectRatio: false,
                animation: { duration: 0 },
                plugins: {
                    legend: { display: false },
                    tooltip: {
                        callbacks: {
                            label: (context) => `延迟: ${context.parsed.y.toFixed(2)}ms`
                        }
                    }
                },
                scales: {
                    x: {
                        type: 'time',
                        time: {
                            unit: 'second',
                            displayFormats: { second: 'HH:mm:ss' }
                        },
                        min: () => new Date(Date.now() - 60000),
                        max: () => new Date(Date.now() + 5000),
                        ticks: {
                            maxRotation: 0,
                            maxTicksLimit: 10
                        }
                    },
                    y: {
                        beginAtZero: true,
                        title: { text: '耗时 (ms)' }
                    }
                }
            }
        });

        // 3. 成功率图表
        const successCtx = document.getElementById('success-chart').getContext('2d');
        this.charts.success = new Chart(successCtx, {
            type: 'line',
            data: {
                datasets: [{
                    label: '成功率 (%)',
                    data: [],
                    borderColor: '#10b981',
                    backgroundColor: 'rgba(16, 185, 129, 0.1)',
                    fill: true,
                    tension: 0.2,
                    borderWidth: 2,
                    pointRadius: 0
                }]
            },
            options: {
                responsive: true,
                maintainAspectRatio: false,
                animation: { duration: 0 },
                plugins: {
                    legend: { display: false }
                },
                scales: {
                    x: {
                        type: 'time',
                        time: {
                            unit: 'second',
                            displayFormats: { second: 'HH:mm:ss' }
                        },
                        min: () => new Date(Date.now() - 60000),
                        max: () => new Date(Date.now() + 5000),
                        ticks: {
                            maxRotation: 0,
                            maxTicksLimit: 10
                        }
                    },
                    y: {
                        beginAtZero: true,
                        max: 100,
                        title: { text: '成功率 (%)' },
                        ticks: {
                            callback: (value) => `${value}%`
                        }
                    }
                }
            }
        });

        // 4. 分布饼图
        const distCtx = document.getElementById('distribution-chart').getContext('2d');
        this.charts.distribution = new Chart(distCtx, {
            type: 'doughnut',
            data: {
                labels: ['等待数据...'],
                datasets: [{
                    data: [100],
                    backgroundColor: ['#f3f4f6'],
                    borderWidth: 2,
                    borderColor: 'white'
                }]
            },
            options: {
                responsive: true,
                maintainAspectRatio: false,
                plugins: {
                    legend: {
                        position: 'right',
                        labels: {
                            padding: 20,
                            usePointStyle: true,
                            font: { size: 12 }
                        }
                    },
                    tooltip: {
                        callbacks: {
                            label: (context) => {
                                const label = context.label || '';
                                const value = context.raw || 0;
                                const total = context.dataset.data.reduce((a, b) => a + b, 0);
                                const percentage = total > 0 ? (value / total * 100).toFixed(1) : 0;
                                const count = this.apiDistribution.counts[context.dataIndex] || 0;
                                return `${label}: ${count}次 (${percentage}%)`;
                            }
                        }
                    }
                }
            }
        });


        // 启动图表刷新定时器
        this.startChartRefresh();
    }

    // 定期刷新图表的时间范围
    startChartRefresh() {
        setInterval(() => {
            // 更新所有图表的时间范围
            Object.values(this.charts).forEach(chart => {
                if (chart.options.scales && chart.options.scales.x) {
                    try {
                        chart.update('none');
                    } catch (error) {
                        console.error('Chart refresh error:', error);
                    }
                }
            });
        }, 10000); // 每10秒刷新一次
    }

// 定期刷新图表的时间范围
    startChartRefresh() {
        setInterval(() => {
            // 更新所有图表的时间范围
            Object.values(this.charts).forEach(chart => {
                if (chart.options.scales && chart.options.scales.x) {
                    try {
                        chart.update('none');
                    } catch (error) {
                        console.error('Chart refresh error:', error);
                    }
                }
            });
        }, 10000); // 每10秒刷新一次
    }

    async initApiList() {
        const list = document.getElementById('api-list');
        const response = await fetch('/api/list', {
            method: 'POST', headers: {'Content-Type': 'application/json'}
        });
        if (response.ok) {
            const data = await response.json();
            (data || []).forEach(api => {
                var item = document.createElement('div');
                item.className = 'api-item';
                item.innerHTML = `
                <h4>${api.name}</h4>
                <p>⚖️ 权重: ${api.weight}%</p>
            `;
                list.appendChild(item);
            });
        }
    }


    bindEvents() {
        // 开始测试
        document.getElementById('start-test').addEventListener('click', () => {
            this.startTest();
        });

        // 停止测试
        document.getElementById('stop-test').addEventListener('click', () => {
            this.stopTest();
        });

        // 重置统计
        document.getElementById('reset-stats').addEventListener('click', () => {
            this.resetStats();
        });
    }

    async startTest() {
        if (this.isRunning) return;

        const config = {
            duration: document.getElementById('duration').value,
            concurrency: parseInt(document.getElementById('concurrency').value),
            rateLimit: parseInt(document.getElementById('rateLimit').value),
        };

        try {
            const response = await fetch('/api/start', {
                method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(config)
            });

            if (response.ok) {
                this.isRunning = true;
                document.getElementById('start-test').disabled = true;
                document.getElementById('stop-test').disabled = false;
                this.addLog('压测已开始', 'info');

                // 重置图表数据
                this.resetChartData();
            }
        } catch (error) {
            this.addLog(`启动失败: ${error.message}`, 'error');
        }
    }

    async stopTest() {
        try {
            const response = await fetch('/api/stop', {method: 'POST'});
            if (response.ok) {
                this.isRunning = false;
                document.getElementById('start-test').disabled = false;
                document.getElementById('stop-test').disabled = true;
                this.addLog('压测已停止', 'info');
            }
        } catch (error) {
            this.addLog(`停止失败: ${error.message}`, 'error');
        }
    }

    async resetStats() {
        try {
            const response = await fetch('/api/reset', {method: 'POST'});
            if (response.ok) {
                this.addLog('统计已重置', 'info');

                // 重置前端统计数据
                this.resetChartData();
                this.updateUIStats({
                    success: 0, failure: 0, tps: 0, avgLatency: 0, minLatency: 0, maxLatency: 0, successRate: 0
                });

                // 清空详细统计表格
                this.updateDetailedStatsTable({});
            }
        } catch (error) {
            this.addLog(`重置失败: ${error.message}`, 'error');
        }
    }

    resetChartData() {
        // 重置所有图表数据
        Object.values(this.charts).forEach(chart => {
            chart.data.datasets.forEach(dataset => {
                dataset.data = [];
            });
            chart.update('none');
        });

        // 重置分布图表为初始状态
        const distChart = this.charts.distribution;
        if (distChart) {
            distChart.data.labels = ['等待数据...'];
            distChart.data.datasets[0].data = [100];
            distChart.data.datasets[0].backgroundColor = ['#f3f4f6'];
            distChart.update();
        }

        this.historicalData = {
            timestamps: [],
            tps: [],
            latencies: [],
            successRates: []
        };

        this.apiDistribution = {
            labels: [],
            data: [],
            counts: []
        };
    }

    connectWebSocket() {
        this.ws = new WebSocket(`ws://${window.location.host}/ws`);

        this.ws.onopen = () => {
            this.addLog('WebSocket连接已建立', 'info');
            if (this.debugMode) console.log('WebSocket connected');
        };

        this.ws.onmessage = async (event) => {
            try {
                const data = JSON.parse(await event.data.text());

                if (this.debugMode) {
                    console.log('WebSocket data received:', {
                        type: data.type,
                        timestamp: new Date().toISOString(),
                        tps: data.realtime?.tps,
                        dataLength: JSON.stringify(data).length
                    });
                }

                if (data.type === 'init') {
                    this.handleInitialData(data);
                } else if (data.type === 'realtime') {
                    this.updateRealtimeStats(data);
                }
            } catch (error) {
                console.error('WebSocket data parse error:', error, event.data);
            }
        };

        this.ws.onclose = () => {
            this.addLog('WebSocket连接断开，正在重连...', 'error');
            if (this.debugMode) console.log('WebSocket disconnected, reconnecting...');
            setTimeout(() => this.connectWebSocket(), 1000);
        };

        this.ws.onerror = (error) => {
            console.error('WebSocket error:', error);
        };
    }


    handleInitialData(data) {
        if (data.historical) {
            this.historicalData = data.historical;
            this.updateHistoricalCharts();
        }

        if (data.api_stats) {
            this.updateDetailedStatsTable(data.api_stats);
        }

        if (data.distribution) {
            this.updateDistributionChart(data.distribution);
        }
    }

    updateRealtimeStats(data) {
        const realtime = data.realtime;

        if (!realtime) {
            console.warn('No realtime data:', data);
            return;
        }

        // 验证和修复数据
        const tps = parseFloat(realtime.tps) || 0;
        const success = parseInt(realtime.success) || 0;
        const failure = parseInt(realtime.failure) || 0;
        const total = parseInt(realtime.total) || 0;
        const avgLatency = parseFloat(realtime.avgLatency) || 0;
        const minLatency = parseFloat(realtime.minLatency) || 0;
        const maxLatency = parseFloat(realtime.maxLatency) || 0;

        // 处理成功率
        let successRate = parseFloat(realtime.successRate);
        if (isNaN(successRate) || successRate === undefined) {
            successRate = total > 0 ? (success / total * 100) : 0;
        }
        successRate = Math.max(0, Math.min(100, successRate));

        // 修复时间戳问题
        let timestamp;
        if (realtime.timestamp) {
            // 确保时间戳是合理的时间（在最近几年内）
            const ts = parseInt(realtime.timestamp);
            const now = Date.now() / 1000;

            if (ts > 1600000000 && ts < now + 86400) { // 2020年以后且不超过明天
                timestamp = new Date(ts * 1000);
            } else {
                // 时间戳不合理，使用当前时间
                console.warn('Invalid timestamp:', ts, 'using current time');
                timestamp = new Date();
            }
        } else {
            timestamp = new Date();
        }

        // 确保时间戳是有效的Date对象
        if (isNaN(timestamp.getTime())) {
            timestamp = new Date();
        }

        if (this.debugMode) {
            console.log('Processed realtime data:', {
                tps, success, failure, total,
                successRate, avgLatency, minLatency, maxLatency,
                timestamp: timestamp.toISOString()
            });
        }

        // 更新UI
        this.updateUIStats({
            success: success,
            failure: failure,
            tps: tps,
            avgLatency: avgLatency,
            minLatency: minLatency,
            maxLatency: maxLatency,
            successRate: successRate
        });

        // 更新图表 - 确保时间戳有效
        this.updateChartData('tps', timestamp, tps);
        this.updateChartData('latency', timestamp, avgLatency);
        this.updateChartData('success', timestamp, successRate);

        // 更新其他统计
        if (data.detailed && data.detailed.api_stats) {
            this.updateDetailedStatsTable(data.detailed.api_stats);
        }

        if (data.distribution) {
            this.updateDistributionChart(data.distribution);
        }

        // 控制日志频率
        const now = Date.now();
        if (!this.lastLogTime || now - this.lastLogTime > 1000) {
            this.addLog(`TPS: ${tps.toFixed(2)} | 成功: ${success} | 失败: ${failure} | 成功率: ${successRate.toFixed(2)}%`);
            this.lastLogTime = now;
        }
    }


    updateDistributionChart(distributionData) {
        const chart = this.charts.distribution;
        if (!chart) return;

        const labels = [];
        const data = [];
        const counts = [];
        const colors = [
            '#3b82f6', '#10b981', '#f59e0b', '#8b5cf6',
            '#ec4899', '#14b8a6', '#f97316', '#6366f1',
            '#84cc16', '#ef4444', '#0ea5e9', '#f43f5e'
        ];

        // 转换数据格式
        Object.entries(distributionData).forEach(([apiName, stats], index) => {
            labels.push(apiName);
            data.push(stats.count || 0);
            counts.push(stats.count || 0);
        });

        // 如果没有数据，显示提示
        if (labels.length === 0) {
            chart.data.labels = ['暂无数据'];
            chart.data.datasets[0].data = [100];
            chart.data.datasets[0].backgroundColor = ['#f3f4f6'];
        } else {
            chart.data.labels = labels;
            chart.data.datasets[0].data = data;

            // 分配颜色
            const backgroundColors = labels.map((_, i) =>
                colors[i % colors.length]
            );
            chart.data.datasets[0].backgroundColor = backgroundColors;
        }

        // 保存数据以便在tooltip中使用
        this.apiDistribution = {
            labels: labels,
            data: data,
            counts: counts
        };

        // 更新图表
        chart.update();
    }

    updateUIStats(stats) {
        // 格式化延迟显示
        const formatLatency = (ms) => {
            if (ms < 1) {
                return `${ms.toFixed(3)}ms`;
            } else if (ms < 1000) {
                return `${ms.toFixed(2)}ms`;
            } else {
                return `${(ms / 1000).toFixed(2)}s`;
            }
        };

        // 更新统计卡片
        document.getElementById('success-count').textContent =
            stats.success.toLocaleString();
        document.getElementById('failure-count').textContent =
            stats.failure.toLocaleString();
        document.getElementById('tps').textContent =
            stats.tps.toFixed(2);
        document.getElementById('avg-latency').textContent =
            formatLatency(stats.avgLatency);

        if (stats.minLatency !== undefined) {
            document.getElementById('min-latency').textContent =
                formatLatency(stats.minLatency);
        }

        if (stats.maxLatency !== undefined) {
            document.getElementById('max-latency').textContent =
                formatLatency(stats.maxLatency);
        }

        document.getElementById('success-rate').textContent =
            `${stats.successRate}%`;
        document.getElementById('failure-rate').textContent =
            `${(100 - stats.successRate).toFixed(2)}%`;

        const total = stats.success + stats.failure;
        document.getElementById('total-requests').textContent =
            total.toLocaleString();
    }

    updateChartData(chartName, timestamp, value) {
        const chart = this.charts[chartName];
        if (!chart) return;

        // 确保值是有效的数字
        if (value === null || value === undefined || isNaN(value)) {
            console.warn(`Invalid ${chartName} value:`, value);
            return;
        }

        // 确保时间戳是有效的
        if (!(timestamp instanceof Date) || isNaN(timestamp.getTime())) {
            console.warn(`Invalid timestamp for ${chartName}:`, timestamp);
            timestamp = new Date();
        }

        // 检查时间戳是否合理（在最近时间内）
        const now = new Date();
        const oneHourAgo = new Date(now.getTime() - 3600000);
        const oneHourFuture = new Date(now.getTime() + 3600000);

        if (timestamp < oneHourAgo || timestamp > oneHourFuture) {
            console.warn(`Timestamp out of range for ${chartName}:`, timestamp);
            timestamp = new Date();
        }

        const dataPoint = {
            x: timestamp,
            y: parseFloat(value)
        };

        // 添加到数据集
        const dataset = chart.data.datasets[0];
        dataset.data.push(dataPoint);

        // 保持合适的数据量
        const maxPoints = 120; // 保留120个数据点（2分钟，每秒1个）
        if (dataset.data.length > maxPoints) {
            dataset.data.shift();
        }

        // 更新图表，但限制频率
        if (!this.lastChartUpdate || Date.now() - this.lastChartUpdate > 100) {
            try {
                chart.update('none');
                this.lastChartUpdate = Date.now();
            } catch (error) {
                console.error(`Chart update error for ${chartName}:`, error);

                // 图表更新失败时，尝试重置图表
                this.repairChart(chart);
            }
        }
    }

// 修复图表的辅助方法
    repairChart(chart) {
        try {
            // 清空数据并重新配置
            chart.data.datasets.forEach(dataset => {
                dataset.data = [];
            });
            chart.data.labels = [];

            // 重新配置时间轴
            if (chart.options.scales && chart.options.scales.x && chart.options.scales.x.type === 'time') {
                const now = new Date();
                chart.options.scales.x.min = new Date(now.getTime() - 60000); // 最近1分钟
                chart.options.scales.x.max = new Date(now.getTime() + 10000); // 未来10秒
            }

            chart.update();
        } catch (error) {
            console.error('Failed to repair chart:', error);
        }
    }

    updateHistoricalCharts() {
        // 如果有历史数据，可以在这里处理
    }

    updateDetailedStatsTable(apiStats) {
        const tbody = document.getElementById('api-stats-body');
        tbody.innerHTML = '';

        // 先收集所有API的数据
        const apiStatsArray = [];
        for (const [apiName, stats] of Object.entries(apiStats)) {
            apiStatsArray.push({
                name: apiName,
                stats: stats
            });
        }

        // 按请求次数排序
        apiStatsArray.sort((a, b) => b.stats.Total - a.stats.Total);

        apiStatsArray.forEach((item) => {
            const stats = item.stats;
            const row = document.createElement('tr');

            // 格式化数字显示
            const formatNumber = (num) => {
                return num.toLocaleString('zh-CN', {
                    minimumFractionDigits: 0,
                    maximumFractionDigits: 0
                });
            };

            // 格式化耗时显示
            const formatLatency = (ms) => {
                if (ms === undefined || ms === null) return '0ms';

                if (ms < 1) {
                    return `${ms.toFixed(3)}ms`;
                } else if (ms < 1000) {
                    return `${ms.toFixed(2)}ms`;
                } else if (ms < 60000) {
                    return `${(ms / 1000).toFixed(2)}s`;
                } else {
                    return `${(ms / 60000).toFixed(2)}min`;
                }
            };

            // 格式化成功率
            const successRate = stats.SuccessRate ? stats.SuccessRate.toFixed(2) : '0.00';

            // 格式化耗时
            const minLatency = formatLatency(stats.MinLatency);
            const maxLatency = formatLatency(stats.MaxLatency);
            const avgLatency = formatLatency(stats.AvgLatency);

            row.innerHTML = `
                <td>${stats.Name || item.name}</td>
                <td>${formatNumber(stats.Total || 0)}</td>
                <td>${formatNumber(stats.Success || 0)}</td>
                <td>${formatNumber(stats.Failure || 0)}</td>
                <td class="success-rate">${successRate}%</td>
                <td class="latency">${minLatency}</td>
                <td class="latency">${maxLatency}</td>
                <td class="latency">${avgLatency}</td>
                <td>${stats.CurrentTPS ? stats.CurrentTPS.toFixed(2) : '0.00'}</td>
            `;

            tbody.appendChild(row);
        });

        // 如果没有数据，显示提示
        if (apiStatsArray.length === 0) {
            const row = document.createElement('tr');
            row.innerHTML = `
                <td colspan="9" style="text-align: center; color: #94a3b8; padding: 40px;">
                    暂无统计数据，请开始压测
                </td>
            `;
            tbody.appendChild(row);
        }
    }

    async updateDetailedStats() {
        try {
            const response = await fetch('/api/detailed-stats');
            if (response.ok) {
                const data = await response.json();
                this.updateDetailedStatsTable(data.api_stats || {});
            }
        } catch (error) {
            console.error('Failed to update detailed stats:', error);
        }

        // 每3秒更新一次详细统计
        setTimeout(() => this.updateDetailedStats(), 3000);
    }

    addLog(message, type = 'info') {
        const logDiv = document.getElementById('log-content');
        const logEntry = document.createElement('div');

        const timestamp = new Date().toLocaleTimeString();
        logEntry.className = `log-entry ${type}`;
        logEntry.innerHTML = `
            <span class="timestamp">[${timestamp}]</span> ${message}
        `;

        logDiv.appendChild(logEntry);
        logDiv.scrollTop = logDiv.scrollHeight;

        // 保留最近200条日志
        const logs = logDiv.querySelectorAll('.log-entry');
        if (logs.length > 200) {
            logs[0].remove();
        }
    }
}

// 初始化应用
document.addEventListener('DOMContentLoaded', () => {
    window.app = new StressTestUI();
});