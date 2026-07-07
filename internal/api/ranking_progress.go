package api

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/tristan-reig/ft-moulinette/internal/pool"
)

// Géométrie des graphes de progression (viewBox SVG).
const (
	chartW = 700.0
	chartH = 280.0
	plotL  = 64.0
	plotR  = 16.0
	plotT  = 16.0
	plotB  = 36.0
)

// Palette de secours quand l'API 42 ne fournit pas de couleur de coalition.
var fallbackColors = []string{"#00babc", "#f1c40f", "#e74c3c", "#3498db", "#a855f7"}

// accentColor est la couleur de la courbe de l'utilisateur connecté.
const accentColor = "#a855f7"

type chartDot struct {
	X, Y float64
}

type chartTick struct {
	X, Y  float64
	Label string
}

type progressSeries struct {
	Label    string
	Color    string
	Polyline string
	Dots     []chartDot
}

type progressChart struct {
	Title   string
	PlotX   float64
	PlotX2  float64
	YLabelX float64
	XLabelY float64
	Series  []progressSeries
	YTicks  []chartTick
	XTicks  []chartTick
	// DataJSON porte les données d'interaction (colonnes, points, valeurs
	// formatées) consommées par ranking_chart.js pour le survol / la légende.
	DataJSON string
}

// Structures sérialisées vers le client pour l'interactivité (attribut
// data-chart). json.Marshal échappe <, > et & : sûr dans un attribut HTML.
type chartPointJSON struct {
	Col int     `json:"col"`
	X   float64 `json:"x"`
	Y   float64 `json:"y"`
	V   string  `json:"v"`
}

type chartSeriesJSON struct {
	Label  string           `json:"label"`
	Color  string           `json:"color"`
	Points []chartPointJSON `json:"points"`
}

type chartColumnJSON struct {
	X   float64 `json:"x"`
	Day string  `json:"day"`
}

type chartDataJSON struct {
	PlotX   float64           `json:"plotX"`
	PlotX2  float64           `json:"plotX2"`
	PlotT   float64           `json:"plotT"`
	PlotB   float64           `json:"plotB"`
	Columns []chartColumnJSON `json:"columns"`
	Series  []chartSeriesJSON `json:"series"`
}

// seriesPoint est une valeur brute (jour → valeur) avant mise à l'échelle.
type seriesPoint struct {
	day   int
	value float64
}

type rawSeries struct {
	label  string
	color  string
	points []seriesPoint
}

func round1(v float64) float64 {
	return math.Round(v*10) / 10
}

// buildChart met à l'échelle des séries brutes dans la géométrie SVG et
// produit à la fois le rendu (ticks, polylignes, points) et les données
// d'interaction (DataJSON). pointFormat formate la valeur exacte d'un point
// (tooltip) ; yFormat formate les graduations de l'axe Y.
func buildChart(title string, days []string, series []rawSeries, yFormat, pointFormat func(float64) string) progressChart {
	n := len(days)
	plotW := chartW - plotL - plotR
	plotH := chartH - plotT - plotB

	maxY := 0.0
	for _, s := range series {
		for _, p := range s.points {
			if p.value > maxY {
				maxY = p.value
			}
		}
	}
	if maxY <= 0 {
		maxY = 1
	}
	maxY *= 1.08

	x := func(day int) float64 {
		if n <= 1 {
			return round1(plotL + plotW/2)
		}
		return round1(plotL + plotW*float64(day)/float64(n-1))
	}
	y := func(v float64) float64 {
		return round1(plotT + plotH*(1-v/maxY))
	}

	chart := progressChart{
		Title:   title,
		PlotX:   plotL,
		PlotX2:  plotL + plotW,
		YLabelX: plotL - 10,
		XLabelY: plotT + plotH + 24,
	}

	for _, frac := range []float64{0, 0.5, 1} {
		v := maxY * frac
		chart.YTicks = append(chart.YTicks, chartTick{Y: y(v), Label: yFormat(v)})
	}

	xTickDays := []int{0}
	if n >= 3 {
		xTickDays = append(xTickDays, (n-1)/2)
	}
	if n >= 2 {
		xTickDays = append(xTickDays, n-1)
	}
	for _, day := range xTickDays {
		chart.XTicks = append(chart.XTicks, chartTick{X: x(day), Label: shortDate(days[day])})
	}

	data := chartDataJSON{PlotX: plotL, PlotX2: plotL + plotW, PlotT: plotT, PlotB: plotT + plotH}
	for day, d := range days {
		data.Columns = append(data.Columns, chartColumnJSON{X: x(day), Day: shortDate(d)})
	}

	for _, s := range series {
		out := progressSeries{Label: s.label, Color: s.color}
		sj := chartSeriesJSON{Label: s.label, Color: s.color}
		var poly strings.Builder
		for _, p := range s.points {
			dot := chartDot{X: x(p.day), Y: y(p.value)}
			out.Dots = append(out.Dots, dot)
			fmt.Fprintf(&poly, "%g,%g ", dot.X, dot.Y)
			sj.Points = append(sj.Points, chartPointJSON{Col: p.day, X: dot.X, Y: dot.Y, V: pointFormat(p.value)})
		}
		if len(s.points) >= 2 {
			out.Polyline = strings.TrimSpace(poly.String())
		}
		chart.Series = append(chart.Series, out)
		data.Series = append(data.Series, sj)
	}

	if raw, err := json.Marshal(data); err == nil {
		chart.DataJSON = string(raw)
	}
	return chart
}

// shortDate convertit AAAA-MM-JJ en JJ/MM pour les libellés d'axe.
func shortDate(date string) string {
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return date
	}
	return t.Format("02/01")
}

func formatLevel(v float64) string { return fmt.Sprintf("%.2f", v) }

func formatScore(v float64) string {
	return fmt.Sprintf("%d pts", int(math.Round(v)))
}

// buildProgressCharts agrège les relevés quotidiens en deux graphes :
// niveau moyen par coalition et score total par coalition, plus la courbe
// de niveau de l'utilisateur connecté s'il figure dans les relevés.
func buildProgressCharts(snaps []pool.Snapshot, userLogin string) []progressChart {
	if len(snaps) == 0 {
		return nil
	}

	days := make([]string, len(snaps))
	for i, s := range snaps {
		days[i] = s.Date
	}

	// Coalitions dans l'ordre de première apparition, avec leur couleur.
	var names []string
	colors := map[string]string{}
	for _, snap := range snaps {
		for _, row := range snap.Rows {
			if _, seen := colors[row.Coalition]; !seen {
				names = append(names, row.Coalition)
				colors[row.Coalition] = row.Color
			}
		}
	}
	for i, name := range names {
		if colors[name] == "" {
			colors[name] = fallbackColors[i%len(fallbackColors)]
		}
	}

	levelSeries := make([]rawSeries, len(names))
	scoreSeries := make([]rawSeries, len(names))
	for i, name := range names {
		levelSeries[i] = rawSeries{label: name, color: colors[name]}
		scoreSeries[i] = rawSeries{label: name, color: colors[name]}
	}
	var userPoints []seriesPoint

	for day, snap := range snaps {
		type agg struct {
			levelSum float64
			scoreSum float64
			count    int
		}
		byCoalition := map[string]*agg{}
		for _, row := range snap.Rows {
			a := byCoalition[row.Coalition]
			if a == nil {
				a = &agg{}
				byCoalition[row.Coalition] = a
			}
			a.levelSum += row.Level
			a.scoreSum += float64(row.Score)
			a.count++

			if row.Login == userLogin {
				userPoints = append(userPoints, seriesPoint{day: day, value: row.Level})
			}
		}
		for i, name := range names {
			if a := byCoalition[name]; a != nil && a.count > 0 {
				levelSeries[i].points = append(levelSeries[i].points, seriesPoint{day: day, value: a.levelSum / float64(a.count)})
				scoreSeries[i].points = append(scoreSeries[i].points, seriesPoint{day: day, value: a.scoreSum})
			}
		}
	}

	withUser := levelSeries
	if len(userPoints) > 0 {
		withUser = append(withUser, rawSeries{
			label:  "★ " + userLogin,
			color:  accentColor,
			points: userPoints,
		})
	}

	return []progressChart{
		buildChart("Niveau moyen par coalition", days, withUser, func(v float64) string {
			return fmt.Sprintf("%.1f", v)
		}, formatLevel),
		buildChart("Score total par coalition", days, scoreSeries, func(v float64) string {
			if v >= 1000 {
				return fmt.Sprintf("%.1fk", v/1000)
			}
			return fmt.Sprintf("%.0f", v)
		}, formatScore),
	}
}
