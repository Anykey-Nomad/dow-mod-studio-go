package fda

import "math"

/*
 * This file is a Go translation of mixfft.c v1 by Jens Jorgen Nielsen.
 * Ported from the vgmstream project.
 *
 * ------------------------------------------------------------------------
 * Original Author:
 *     Jens Joergen Nielsen            For non-commercial use only.
 *     Bakkehusene 54                  A $100 fee must be paid if used
 *     DK-2970 Hoersholm               commercially. Please contact.
 *     DENMARK
 *
 *     E-mail : jjn@get2net.dk   All rights reserved. October 2000.
 * ------------------------------------------------------------------------
 */

const (
	maxPrimeFactor    = 37
	maxPrimeFactorDiv = (maxPrimeFactor + 1) / 2
	maxFactorCount    = 20
)

var (
	c3_1 = float32(-1.5)                        // cos(2*pi/3)-1
	c3_2 = float32(0.866025388240814208984375)  // sin(2*pi/3)
	c5_1 = float32(-1.25)                       // (cos(u5)+cos(2*u5))/2-1
	c5_2 = float32(0.559017002582550048828125)  // (cos(u5)-cos(2*u5))/2
	c5_3 = float32(-0.951056540012359619140625) // -sin(u5)
	c5_4 = float32(-1.538841724395751953125)    // -(sin(u5)+sin(2*u5))
	c5_5 = float32(0.3632712662220001220703125) // (sin(u5)-sin(2*u5))
	c8   = float32(0.707106769084930419921875)  // 1/sqrt(2)
	mpi  = float32(3.1415927410125732421875)
)

func fftFactorize(n int, fact *[maxFactorCount]int) int {
	radices := [7]int{1, 2, 3, 4, 5, 8, 10}
	factors := [maxFactorCount]int{}
	nFact := 0
	j := 0

	if n == 1 {
		j = 1
		factors[1] = 1
	}

	i := 6
	for n > 1 && i > 0 {
		if n%radices[i] == 0 {
			n /= radices[i]
			j++
			factors[j] = radices[i]
		} else {
			i--
		}
	}

	// substitute factors 2*8 with 4*4
	if factors[j] == 2 {
		i = j - 1
		for i > 0 && factors[i] != 8 {
			i--
		}
		if i > 0 {
			factors[j] = 4
			factors[i] = 4
		}
	}

	if n > 1 {
		for k := 2; k <= int(math.Sqrt(float64(n)))+1; k++ {
			for n%k == 0 {
				n /= k
				j++
				factors[j] = k
			}
		}
		if n > 1 {
			j++
			factors[j] = n
		}
	}

	for i := 1; i <= j; i++ {
		fact[i] = factors[j-i+1]
	}
	nFact = j
	return nFact
}

func fftTransTableSetup(sofar, actual, remain *[maxFactorCount]int, nPoints int) int {
	nFact := fftFactorize(nPoints, actual)
	if actual[nFact] > maxPrimeFactor {
		actual[nFact] = maxPrimeFactor - 1
	}
	remain[0] = nPoints
	sofar[1] = 1
	remain[1] = nPoints / actual[1]
	for i := 2; i <= nFact; i++ {
		sofar[i] = sofar[i-1] * actual[i-1]
		remain[i] = remain[i-1] / actual[i]
	}
	return nFact
}

func fftPermute(nPoint, nFact int, fact, remain *[maxFactorCount]int, xRe, xIm, yRe, yIm []float32) {
	count := [maxFactorCount]int{}
	k := 0
	for i := 0; i <= nPoint-2; i++ {
		yRe[i] = xRe[k]
		yIm[i] = xIm[k]
		j := 1
		k += remain[j]
		count[1]++
		for count[j] >= fact[j] {
			count[j] = 0
			k = k - remain[j-1] + remain[j+1]
			j++
			count[j]++
		}
	}
	yRe[nPoint-1] = xRe[nPoint-1]
	yIm[nPoint-1] = xIm[nPoint-1]
}

func fftInitTrig(radix int, trigRe, trigIm []float32) {
	w := 2.0 * mpi / float32(radix)
	trigRe[0] = 1
	trigIm[0] = 0
	xre := float32(math.Cos(float64(w)))
	xim := float32(-math.Sin(float64(w)))
	trigRe[1] = xre
	trigIm[1] = xim
	for i := 2; i < radix; i++ {
		trigRe[i] = xre*trigRe[i-1] - xim*trigIm[i-1]
		trigIm[i] = xim*trigRe[i-1] + xre*trigIm[i-1]
	}
}

func fft4(aRe, aIm []float32) {
	t1_re := aRe[0] + aRe[2]
	t1_im := aIm[0] + aIm[2]
	t2_re := aRe[1] + aRe[3]
	t2_im := aIm[1] + aIm[3]
	m2_re := aRe[0] - aRe[2]
	m2_im := aIm[0] - aIm[2]
	m3_re := aIm[1] - aIm[3]
	m3_im := aRe[3] - aRe[1]
	aRe[0] = t1_re + t2_re
	aIm[0] = t1_im + t2_im
	aRe[2] = t1_re - t2_re
	aIm[2] = t1_im - t2_im
	aRe[1] = m2_re + m3_re
	aIm[1] = m2_im + m3_im
	aRe[3] = m2_re - m3_re
	aIm[3] = m2_im - m3_im
}

func fft5(aRe, aIm []float32) {
	t1_re := aRe[1] + aRe[4]
	t1_im := aIm[1] + aIm[4]
	t2_re := aRe[2] + aRe[3]
	t2_im := aIm[2] + aIm[3]
	t3_re := aRe[1] - aRe[4]
	t3_im := aIm[1] - aIm[4]
	t4_re := aRe[3] - aRe[2]
	t4_im := aIm[3] - aIm[2]
	t5_re := t1_re + t2_re
	t5_im := t1_im + t2_im
	aRe[0] += t5_re
	aIm[0] += t5_im
	m1_re := c5_1 * t5_re
	m1_im := c5_1 * t5_im
	m2_re := c5_2 * (t1_re - t2_re)
	m2_im := c5_2 * (t1_im - t2_im)
	m3_re := -c5_3 * (t3_im + t4_im)
	m3_im := c5_3 * (t3_re + t4_re)
	m4_re := -c5_4 * t4_im
	m4_im := c5_4 * t4_re
	m5_re := -c5_5 * t3_im
	m5_im := c5_5 * t3_re
	s3_re := m3_re - m4_re
	s3_im := m3_im - m4_im
	s5_re := m3_re + m5_re
	s5_im := m3_im + m5_im
	s1_re := aRe[0] + m1_re
	s1_im := aIm[0] + m1_im
	s2_re := s1_re + m2_re
	s2_im := s1_im + m2_im
	s4_re := s1_re - m2_re
	s4_im := s1_im - m2_im
	aRe[1] = s2_re + s3_re
	aIm[1] = s2_im + s3_im
	aRe[2] = s4_re + s5_re
	aIm[2] = s4_im + s5_im
	aRe[3] = s4_re - s5_re
	aIm[3] = s4_im - s5_im
	aRe[4] = s2_re - s3_re
	aIm[4] = s2_im - s3_im
}

func fft8(zRe, zIm []float32) {
	var aRe, aIm, bRe, bIm [4]float32
	aRe[0] = zRe[0]
	bRe[0] = zRe[1]
	aRe[1] = zRe[2]
	bRe[1] = zRe[3]
	aRe[2] = zRe[4]
	bRe[2] = zRe[5]
	aRe[3] = zRe[6]
	bRe[3] = zRe[7]
	aIm[0] = zIm[0]
	bIm[0] = zIm[1]
	aIm[1] = zIm[2]
	bIm[1] = zIm[3]
	aIm[2] = zIm[4]
	bIm[2] = zIm[5]
	aIm[3] = zIm[6]
	bIm[3] = zIm[7]

	fft4(aRe[:], aIm[:])
	fft4(bRe[:], bIm[:])

	gem := c8 * (bRe[1] + bIm[1])
	bIm[1] = c8 * (bIm[1] - bRe[1])
	bRe[1] = gem
	gem = bIm[2]
	bIm[2] = -bRe[2]
	bRe[2] = gem
	gem = c8 * (bIm[3] - bRe[3])
	bIm[3] = -c8 * (bRe[3] + bIm[3])
	bRe[3] = gem

	zRe[0] = aRe[0] + bRe[0]
	zRe[4] = aRe[0] - bRe[0]
	zRe[1] = aRe[1] + bRe[1]
	zRe[5] = aRe[1] - bRe[1]
	zRe[2] = aRe[2] + bRe[2]
	zRe[6] = aRe[2] - bRe[2]
	zRe[3] = aRe[3] + bRe[3]
	zRe[7] = aRe[3] - bRe[3]
	zIm[0] = aIm[0] + bIm[0]
	zIm[4] = aIm[0] - bIm[0]
	zIm[1] = aIm[1] + bIm[1]
	zIm[5] = aIm[1] - bIm[1]
	zIm[2] = aIm[2] + bIm[2]
	zIm[6] = aIm[2] - bIm[2]
	zIm[3] = aIm[3] + bIm[3]
	zIm[7] = aIm[3] - bIm[3]
}

func fft10(zRe, zIm []float32) {
	var aRe, aIm, bRe, bIm [5]float32
	aRe[0] = zRe[0]
	bRe[0] = zRe[5]
	aRe[1] = zRe[2]
	bRe[1] = zRe[7]
	aRe[2] = zRe[4]
	bRe[2] = zRe[9]
	aRe[3] = zRe[6]
	bRe[3] = zRe[1]
	aRe[4] = zRe[8]
	bRe[4] = zRe[3]
	aIm[0] = zIm[0]
	bIm[0] = zIm[5]
	aIm[1] = zIm[2]
	bIm[1] = zIm[7]
	aIm[2] = zIm[4]
	bIm[2] = zIm[9]
	aIm[3] = zIm[6]
	bIm[3] = zIm[1]
	aIm[4] = zIm[8]
	bIm[4] = zIm[3]

	fft5(aRe[:], aIm[:])
	fft5(bRe[:], bIm[:])

	zRe[0] = aRe[0] + bRe[0]
	zRe[5] = aRe[0] - bRe[0]
	zRe[6] = aRe[1] + bRe[1]
	zRe[1] = aRe[1] - bRe[1]
	zRe[2] = aRe[2] + bRe[2]
	zRe[7] = aRe[2] - bRe[2]
	zRe[8] = aRe[3] + bRe[3]
	zRe[3] = aRe[3] - bRe[3]
	zRe[4] = aRe[4] + bRe[4]
	zRe[9] = aRe[4] - bRe[4]
	zIm[0] = aIm[0] + bIm[0]
	zIm[5] = aIm[0] - bIm[0]
	zIm[6] = aIm[1] + bIm[1]
	zIm[1] = aIm[1] - bIm[1]
	zIm[2] = aIm[2] + bIm[2]
	zIm[7] = aIm[2] - bIm[2]
	zIm[8] = aIm[3] + bIm[3]
	zIm[3] = aIm[3] - bIm[3]
	zIm[4] = aIm[4] + bIm[4]
	zIm[9] = aIm[4] - bIm[4]
}

func fftOdd(radix int, trigRe, trigIm, zRe, zIm []float32) {
	n := radix
	max := (n + 1) / 2
	vRe := make([]float32, max)
	vIm := make([]float32, max)
	wRe := make([]float32, max)
	wIm := make([]float32, max)

	for j := 1; j < max; j++ {
		vRe[j] = zRe[j] + zRe[n-j]
		vIm[j] = zIm[j] - zIm[n-j]
		wRe[j] = zRe[j] - zRe[n-j]
		wIm[j] = zIm[j] + zIm[n-j]
	}
	for j := 1; j < max; j++ {
		zRe[j] = zRe[0]
		zIm[j] = zIm[0]
		zRe[n-j] = zRe[0]
		zIm[n-j] = zIm[0]
		k := j
		for i := 1; i < max; i++ {
			rere := trigRe[k] * vRe[i]
			imim := trigIm[k] * vIm[i]
			reim := trigRe[k] * wIm[i]
			imre := trigIm[k] * wRe[i]
			zRe[n-j] += rere + imim
			zIm[n-j] += reim - imre
			zRe[j] += rere - imim
			zIm[j] += reim + imre
			k += j
			if k >= n {
				k -= n
			}
		}
	}
	for j := 1; j < max; j++ {
		zRe[0] += vRe[j]
		zIm[0] += wIm[j]
	}
}

func fftTwiddleTransf(sofarRadix, radix, remainRadix int, yRe, yIm []float32) {
	twiddleRe := make([]float32, maxPrimeFactor)
	twiddleIm := make([]float32, maxPrimeFactor)
	trigRe := make([]float32, maxPrimeFactor)
	trigIm := make([]float32, maxPrimeFactor)
	zRe := make([]float32, maxPrimeFactor)
	zIm := make([]float32, maxPrimeFactor)

	fftInitTrig(radix, trigRe, trigIm)
	omega := 2.0 * mpi / float32(sofarRadix*radix)
	cosw := float32(math.Cos(float64(omega)))
	sinw := float32(-math.Sin(float64(omega)))
	tw_re := float32(1.0)
	tw_im := float32(0.0)
	dataOffset := 0
	groupOffset := dataOffset
	adr := groupOffset

	for dataNo := 0; dataNo < sofarRadix; dataNo++ {
		if sofarRadix > 1 {
			twiddleRe[0] = 1.0
			twiddleIm[0] = 0.0
			twiddleRe[1] = tw_re
			twiddleIm[1] = tw_im
			for twNo := 2; twNo < radix; twNo++ {
				twiddleRe[twNo] = tw_re*twiddleRe[twNo-1] - tw_im*twiddleIm[twNo-1]
				twiddleIm[twNo] = tw_im*twiddleRe[twNo-1] + tw_re*twiddleIm[twNo-1]
			}
			gem := cosw*tw_re - sinw*tw_im
			tw_im = sinw*tw_re + cosw*tw_im
			tw_re = gem
		}
		for groupNo := 0; groupNo < remainRadix; groupNo++ {
			if sofarRadix > 1 && dataNo > 0 {
				zRe[0] = yRe[adr]
				zIm[0] = yIm[adr]
				blockNo := 1
				adr += sofarRadix
				for blockNo < radix {
					zRe[blockNo] = twiddleRe[blockNo]*yRe[adr] - twiddleIm[blockNo]*yIm[adr]
					zIm[blockNo] = twiddleRe[blockNo]*yIm[adr] + twiddleIm[blockNo]*yRe[adr]
					blockNo++
					adr += sofarRadix
				}
			} else {
				for blockNo := 0; blockNo < radix; blockNo++ {
					zRe[blockNo] = yRe[adr]
					zIm[blockNo] = yIm[adr]
					adr += sofarRadix
				}
			}

			var gem float32
			switch radix {
			case 2:
				gem = zRe[0] + zRe[1]
				zRe[1] = zRe[0] - zRe[1]
				zRe[0] = gem
				gem = zIm[0] + zIm[1]
				zIm[1] = zIm[0] - zIm[1]
				zIm[0] = gem
			case 3:
				t1_re := zRe[1] + zRe[2]
				t1_im := zIm[1] + zIm[2]
				zRe[0] += t1_re
				zIm[0] += t1_im
				m1_re := c3_1 * t1_re
				m1_im := c3_1 * t1_im
				m2_re := c3_2 * (zIm[1] - zIm[2])
				m2_im := c3_2 * (zRe[2] - zRe[1])
				s1_re := zRe[0] + m1_re
				s1_im := zIm[0] + m1_im
				zRe[1] = s1_re + m2_re
				zIm[1] = s1_im + m2_im
				zRe[2] = s1_re - m2_re
				zIm[2] = s1_im - m2_im
			case 4:
				t1_re := zRe[0] + zRe[2]
				t1_im := zIm[0] + zIm[2]
				t2_re := zRe[1] + zRe[3]
				t2_im := zIm[1] + zIm[3]
				m2_re := zRe[0] - zRe[2]
				m2_im := zIm[0] - zIm[2]
				m3_re := zIm[1] - zIm[3]
				m3_im := zRe[3] - zRe[1]
				zRe[0] = t1_re + t2_re
				zIm[0] = t1_im + t2_im
				zRe[2] = t1_re - t2_re
				zIm[2] = t1_im - t2_im
				zRe[1] = m2_re + m3_re
				zIm[1] = m2_im + m3_im
				zRe[3] = m2_re - m3_re
				zIm[3] = m2_im - m3_im
			case 5:
				t1_re := zRe[1] + zRe[4]
				t1_im := zIm[1] + zIm[4]
				t2_re := zRe[2] + zRe[3]
				t2_im := zIm[2] + zIm[3]
				t3_re := zRe[1] - zRe[4]
				t3_im := zIm[1] - zIm[4]
				t4_re := zRe[3] - zRe[2]
				t4_im := zIm[3] - zIm[2]
				t5_re := t1_re + t2_re
				t5_im := t1_im + t2_im
				zRe[0] += t5_re
				zIm[0] += t5_im
				m1_re := c5_1 * t5_re
				m1_im := c5_1 * t5_im
				m2_re := c5_2 * (t1_re - t2_re)
				m2_im := c5_2 * (t1_im - t2_im)
				m3_re := -c5_3 * (t3_im + t4_im)
				m3_im := c5_3 * (t3_re + t4_re)
				m4_re := -c5_4 * t4_im
				m4_im := c5_4 * t4_re
				m5_re := -c5_5 * t3_im
				m5_im := c5_5 * t3_re
				s3_re := m3_re - m4_re
				s3_im := m3_im - m4_im
				s5_re := m3_re + m5_re
				s5_im := m3_im + m5_im
				s1_re := zRe[0] + m1_re
				s1_im := zIm[0] + m1_im
				s2_re := s1_re + m2_re
				s2_im := s1_im + m2_im
				s4_re := s1_re - m2_re
				s4_im := s1_im - m2_im
				zRe[1] = s2_re + s3_re
				zIm[1] = s2_im + s3_im
				zRe[2] = s4_re + s5_re
				zIm[2] = s4_im + s5_im
				zRe[3] = s4_re - s5_re
				zIm[3] = s4_im - s5_im
				zRe[4] = s2_re - s3_re
				zIm[4] = s2_im - s3_im
			case 8:
				fft8(zRe[:radix], zIm[:radix])
			case 10:
				fft10(zRe[:radix], zIm[:radix])
			default:
				fftOdd(radix, trigRe, trigIm, zRe, zIm)
			}

			adr = groupOffset
			for blockNo := 0; blockNo < radix; blockNo++ {
				yRe[adr] = zRe[blockNo]
				yIm[adr] = zIm[blockNo]
				adr += sofarRadix
			}
			groupOffset += sofarRadix * radix
			adr = groupOffset
		}
		dataOffset++
		groupOffset = dataOffset
		adr = groupOffset
	}
}

// RelicMixFFT is the main FFT entry point, ported from relic_mixfft.c
func RelicMixFFT(n int, xRe, xIm, yRe, yIm []float32) {
	var sofarRadix, actualRadix, remainRadix [maxFactorCount]int
	nFact := fftTransTableSetup(&sofarRadix, &actualRadix, &remainRadix, n)
	fftPermute(n, nFact, &actualRadix, &remainRadix, xRe, xIm, yRe, yIm)
	for count := 1; count <= nFact; count++ {
		fftTwiddleTransf(sofarRadix[count], actualRadix[count], remainRadix[count], yRe, yIm)
	}
}
