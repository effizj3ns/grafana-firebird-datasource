// uplot mock: jsdom lacks matchMedia which uplot calls at import time.
// The ConfigEditor tests never render charts, so a stub suffices.
module.exports = { uPlot: function uPlot() {} };
