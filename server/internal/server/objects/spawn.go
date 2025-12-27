package objects

import "math/rand/v2"

var getPlayerPosition = func(player *Player) (float64, float64) { return player.X, player.Y }
var getPlayerRadius = func(player *Player) float64 { return player.Radius }
var getSporePosition = func(spore *Spore) (float64, float64) { return spore.X, spore.Y }
var getSporeRadius = func(spore *Spore) float64 { return spore.Radius }

func isTooClose[T any](x float64, y float64, radius float64, objects *SharedCollection[T], getPosition func(T) (float64, float64), getRadius func(T) float64) bool {
	if objects == nil {
		return false
	}

	tooClose := false
	objects.ForEach(func(_ uint64, object T) {
		if tooClose {
			return
		}

		objX, objY := getPosition(object)
		objRad := getRadius(object)
		xDist := objX - x
		yDist := objY - y
		distSq := xDist * xDist + yDist * yDist

		if distSq <= (radius + objRad) * (radius + objRad) {
			tooClose = true
			return
		}
	})

	return tooClose
}

func SpawnCoords(radius float64, playersToAvoid *SharedCollection[*Player], sporesToAvoid *SharedCollection[*Spore]) (float64, float64) {
	bound := 3000.0
	const maxTries int = 25

	tries := 0

	for {
		x := bound * (2 * rand.Float64() - 1)
		y := bound * (2 * rand.Float64() - 1)
		closeToPlayer := isTooClose(x, y, radius, playersToAvoid, getPlayerPosition, getPlayerRadius)
		closeToSpore := isTooClose(x, y, radius, sporesToAvoid, getSporePosition, getSporeRadius)

		if !closeToPlayer && !closeToSpore {
			return x, y
		}

		tries++

		if tries >= maxTries {
			bound *= 2
			tries = 0
		}
	}
}