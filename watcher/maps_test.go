package watcher

import (
	"sort"
	"testing"

	sets "github.com/deckarep/golang-set/v2"
	"github.com/stretchr/testify/assert"
)

func TestWLIDSetNew(t *testing.T) {
	tt := []struct {
		name        string
		inputValues []string
	}{
		{
			name:        "The created set should contain the input values",
			inputValues: []string{"a", "b"},
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			ws := NewWLIDSet(tc.inputValues...)

			expectedValues := sets.NewSet(tc.inputValues...)
			if !expectedValues.Equal(ws) {
				t.Errorf("Given sets are not equal.")

			}
		})
	}
}

func TestImageIDWLIDsMapNew(t *testing.T) {
	iwMap := NewImageHashWLIDsMap()

	assert.NotNilf(t, iwMap, "Returned map should not be nil")
}

func assertRawMapEqualsIWMap(t *testing.T, rawMap map[string][]string, iwMap *imageHashWLIDMap) {
	allKeys := make([]string, 0)

	for k := range rawMap {
		allKeys = append(allKeys, k)
	}
	for k := range iwMap.wlidsByImageHash {
		allKeys = append(allKeys, k)
	}

	for _, k := range allKeys {
		var rawSet, iwSet wlidSet

		rawWlids := rawMap[k]
		rawSet = NewWLIDSet(rawWlids...)

		iwSet, ok := iwMap.LoadSet(k)
		if !ok {
			iwSet = NewWLIDSet()
		}

		if !rawSet.Equal(iwSet) {
			t.Errorf("For key %s, sets are not matching. RawSet: %v, IWSet: %v", k, rawSet, iwSet)
		}
	}
}

func TestImageIDWLIDsMapNewFrom(t *testing.T) {
	tt := []struct {
		name           string
		startingValues map[string][]string
	}{
		{
			name:           "Empty starting values construct an empty map",
			startingValues: map[string][]string{},
		},
		{
			name: "Non-empty starting values construct a matching map",
			startingValues: map[string][]string{
				"7238b08a6bad494e84ed1c632a62d39bdeed1f929950a05c1a32b6d4490a0047": {"wlid-01"},
			},
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			iwMap := NewImageHashWLIDsMapFrom(tc.startingValues)

			assert.NotNilf(t, iwMap, "Returned map should not be nil")
			assertRawMapEqualsIWMap(t, tc.startingValues, iwMap)
		})
	}
}

func TestImageIDWLIDsMapStoreSetAndLoadSet(t *testing.T) {
	tt := []struct {
		name        string
		inputKVs    map[string]wlidSet
		expectedKVs map[string]wlidSet
		expectedOks map[string]bool
	}{
		{
			name: "Storing a single value should return a matching value",
			inputKVs: map[string]wlidSet{
				"someImageID": NewWLIDSet("someWLID"),
			},
			expectedKVs: map[string]wlidSet{
				"someImageID": NewWLIDSet("someWLID"),
			},
			expectedOks: map[string]bool{
				"someImageID": true,
			},
		},
		{
			name: "Storing multiple keys should return matching values",
			inputKVs: map[string]wlidSet{
				"someImageID":      NewWLIDSet("someWLID"),
				"someOtherImageID": NewWLIDSet("someOtherWLID"),
			},
			expectedKVs: map[string]wlidSet{
				"someImageID":      NewWLIDSet("someWLID"),
				"someOtherImageID": NewWLIDSet("someOtherWLID"),
			},
			expectedOks: map[string]bool{
				"someImageID":      true,
				"someOtherImageID": true,
			},
		},
		{
			name:     "Getting from empty map should return a NOT ok flag",
			inputKVs: map[string]wlidSet{},
			expectedKVs: map[string]wlidSet{
				"someImageID": nil,
			},
			expectedOks: map[string]bool{
				"someImageID": false,
			},
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			iwMap := NewImageHashWLIDsMap()

			for k, v := range tc.inputKVs {
				iwMap.StoreSet(k, v)
			}

			actualKVs := map[string]wlidSet{}
			for k := range tc.expectedKVs {
				actualValue, _ := iwMap.LoadSet(k)
				actualKVs[k] = actualValue
			}

			actualOks := map[string]bool{}
			for k := range tc.expectedOks {
				_, ok := iwMap.LoadSet(k)
				actualOks[k] = ok
			}

			assert.Equalf(t, tc.expectedKVs, actualKVs, "Stored value must match the input value")
			assert.Equalf(t, tc.expectedOks, actualOks, "Actual OKs must match the expected OKs")
		})
	}
}

func TestImageIDWLIDsMapLoadSetResultImmutable(t *testing.T) {
	tt := []struct {
		name        string
		startingMap map[string]wlidSet
		appendInput []string
		testKey     string
		expected    wlidSet
	}{
		{
			name: "Plain appending to a Get result of a map should not mutate the underlying map",
			startingMap: map[string]wlidSet{
				"some": NewWLIDSet("first", "second"),
			},
			testKey:     "some",
			appendInput: []string{"INTRUDER"},
			expected:    NewWLIDSet("first", "second"),
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			iwMap := NewImageHashWLIDsMap()
			for k, v := range tc.startingMap {
				iwMap.StoreSet(k, v)
			}
			got, _ := iwMap.LoadSet(tc.testKey)

			for _, input := range tc.appendInput {
				got.Add(input)
			}

			actual, _ := iwMap.LoadSet(tc.testKey)

			if !actual.Equal(tc.expected) {
				t.Errorf("Sets are not equal. Got: %v, want: %v", actual, tc.expected)
			}
		})
	}
}

func TestImageIDWLIDsMapClear(t *testing.T) {
	tt := []struct {
		name           string
		startingValues map[string]wlidSet
	}{
		{
			name: "Clearing a non-empty map should make it an empty map",
			startingValues: map[string]wlidSet{
				"7238b08a6bad494e84ed1c632a62d39bdeed1f929950a05c1a32b6d4490a0047": NewWLIDSet("wlid01", "wlid02"),
			},
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			iwMap := NewImageHashWLIDsMap()
			for k, v := range tc.startingValues {
				iwMap.StoreSet(k, v)
			}

			iwMap.Clear()

			remainingValues := map[string]wlidSet{}
			for k := range tc.startingValues {
				remainingValue, ok := iwMap.LoadSet(k)
				if ok {
					remainingValues[k] = remainingValue
				}
			}
			expectedRemainingValues := map[string]wlidSet{}
			assert.Equal(t, expectedRemainingValues, remainingValues)
		})
	}
}

func TestImageIDWLIDsAdd(t *testing.T) {
	type iwMapAddOperation struct {
		imageHash string
		wlids     []string
	}

	tt := []struct {
		name            string
		startingValues  map[string][]string
		inputOperations []iwMapAddOperation
		expectedMap     map[string][]string
	}{
		{
			name:           "Adding a full imageHash-WLIDs pair to an empty map should be reflected",
			startingValues: map[string][]string{},
			inputOperations: []iwMapAddOperation{
				{"7238b08a6bad494e84ed1c632a62d39bdeed1f929950a05c1a32b6d4490a0047", []string{"wlid-01", "wlid-02"}},
			},
			expectedMap: map[string][]string{
				"7238b08a6bad494e84ed1c632a62d39bdeed1f929950a05c1a32b6d4490a0047": {"wlid-01", "wlid-02"},
			},
		},
		{
			name: "Adding WLIDs to existing imageHash should extend the set of WLIDs",
			startingValues: map[string][]string{
				"7238b08a6bad494e84ed1c632a62d39bdeed1f929950a05c1a32b6d4490a0047": {"wlid-01", "wlid-02"},
			},
			inputOperations: []iwMapAddOperation{
				{"7238b08a6bad494e84ed1c632a62d39bdeed1f929950a05c1a32b6d4490a0047", []string{"wlid-03"}},
			},
			expectedMap: map[string][]string{
				"7238b08a6bad494e84ed1c632a62d39bdeed1f929950a05c1a32b6d4490a0047": {"wlid-01", "wlid-02", "wlid-03"},
			},
		},
		{
			name:           "Adding multiple WLIDs to an empty map should store matching WLIDs",
			startingValues: map[string][]string{},
			inputOperations: []iwMapAddOperation{
				{"7238b08a6bad494e84ed1c632a62d39bdeed1f929950a05c1a32b6d4490a0047", []string{"wlid-01", "wlid-02"}},
				{"7238b08a6bad494e84ed1c632a62d39bdeed1f929950a05c1a32b6d4490a0048", []string{"wlid-03", "wlid-04"}},
			},
			expectedMap: map[string][]string{
				"7238b08a6bad494e84ed1c632a62d39bdeed1f929950a05c1a32b6d4490a0047": {"wlid-01", "wlid-02"},
				"7238b08a6bad494e84ed1c632a62d39bdeed1f929950a05c1a32b6d4490a0048": {"wlid-03", "wlid-04"},
			},
		},
		{
			name: "Adding WLIDs to existing keys should store matching WLIDs",
			startingValues: map[string][]string{
				"7238b08a6bad494e84ed1c632a62d39bdeed1f929950a05c1a32b6d4490a0047": {"wlid-01", "wlid-02"},
				"7238b08a6bad494e84ed1c632a62d39bdeed1f929950a05c1a32b6d4490a0048": {"wlid-03", "wlid-04"},
			},
			inputOperations: []iwMapAddOperation{
				{"7238b08a6bad494e84ed1c632a62d39bdeed1f929950a05c1a32b6d4490a0047", []string{"wlid-05", "wlid-06"}},
				{"7238b08a6bad494e84ed1c632a62d39bdeed1f929950a05c1a32b6d4490a0048", []string{"wlid-07", "wlid-08"}},
			},
			expectedMap: map[string][]string{
				"7238b08a6bad494e84ed1c632a62d39bdeed1f929950a05c1a32b6d4490a0047": {"wlid-01", "wlid-02", "wlid-05", "wlid-06"},
				"7238b08a6bad494e84ed1c632a62d39bdeed1f929950a05c1a32b6d4490a0048": {"wlid-03", "wlid-04", "wlid-07", "wlid-08"},
			},
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			iwMap := NewImageHashWLIDsMapFrom(tc.startingValues)

			for _, op := range tc.inputOperations {
				iwMap.Add(op.imageHash, op.wlids...)
			}

			assertRawMapEqualsIWMap(t, tc.expectedMap, iwMap)
		})
	}
}

func TestImageIDWLIDsMapLoad(t *testing.T) {
	type loadResult struct {
		imageHash     string
		expectedWlids []string
		expectedOk    bool
	}

	tt := []struct {
		name        string
		inputSlices map[string][]string
		testedLoads []loadResult
	}{
		{
			name: "Retrieving a slice after storing produces a slice that contains the same elements",
			testedLoads: []loadResult{
				{
					imageHash:     "7238b08a6bad494e84ed1c632a62d39bdeed1f929950a05c1a32b6d4490a0047",
					expectedWlids: []string{"wlid-01", "wlid-02"},
					expectedOk:    true,
				},
			},
			inputSlices: map[string][]string{
				"7238b08a6bad494e84ed1c632a62d39bdeed1f929950a05c1a32b6d4490a0047": {"wlid-01", "wlid-02"},
			},
		},
		{
			name:        "Retrieving a slice from an empty map should return nil and NOT ok",
			inputSlices: map[string][]string{},
			testedLoads: []loadResult{
				{
					imageHash:     "missing-key",
					expectedWlids: nil,
					expectedOk:    false,
				},
			},
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			iwMap := NewImageHashWLIDsMapFrom(tc.inputSlices)

			for _, tl := range tc.testedLoads {
				loadedWlids, ok := iwMap.Load(tl.imageHash)

				assert.ElementsMatch(t, tl.expectedWlids, loadedWlids)
				assert.Equal(t, tl.expectedOk, ok)
			}
		})
	}
}

func TestImageIDWLIDsMapRange(t *testing.T) {
	tt := []struct {
		name           string
		startingValues map[string][]string
	}{
		{
			name: "Ranging over the map has access to all values",
			startingValues: map[string][]string{
				"7238b08a6bad494e84ed1c632a62d39bdeed1f929950a05c1a32b6d4490a0047": {"wlid-01", "wlid-02"},
				"7238b08a6bad494e84ed1c632a62d39bdeed1f929950a05c1a32b6d4490a0048": {"wlid-03", "wlid-04"},
			},
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			iwMap := NewImageHashWLIDsMapFrom(tc.startingValues)

			visitedKeys := map[string][]string{}
			iwMap.Range(func(imageHash string, wlids []string) bool {
				visitedKeys[imageHash] = wlids
				return true
			})

			// Since
			for imageHash := range visitedKeys {
				sort.Strings(visitedKeys[imageHash])
			}
			assert.Equal(t, tc.startingValues, visitedKeys)
		})
	}
}

func TestImageIDWLIDsMapRangeShortCircuit(t *testing.T) {
	tt := []struct {
		name               string
		startingValues     map[string][]string
		expectedVisitedLen int
	}{
		{
			name: "Ranging over the map has access to values only before returning false",
			startingValues: map[string][]string{
				"7238b08a6bad494e84ed1c632a62d39bdeed1f929950a05c1a32b6d4490a0047": {"wlid-01", "wlid-02"},
				"7238b08a6bad494e84ed1c632a62d39bdeed1f929950a05c1a32b6d4490a0048": {"wlid-03", "wlid-04"},
				"7238b08a6bad494e84ed1c632a62d39bdeed1f929950a05c1a32b6d4490a0049": {"wlid-05", "wlid-06"},
			},
			expectedVisitedLen: 1,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			iwMap := NewImageHashWLIDsMapFrom(tc.startingValues)

			visitedKeys := map[string][]string{}

			iwMap.Range(func(imageHash string, wlids []string) bool {
				visitedKeys[imageHash] = wlids
				return false
			})

			assert.Equal(t, tc.expectedVisitedLen, len(visitedKeys))
		})
	}
}

func TestImageIDWLIDsMapAsMap(t *testing.T) {
	tt := []struct {
		name           string
		startingValues map[string][]string
	}{
		{
			name: "Getting as map should return expected values",
			startingValues: map[string][]string{
				"7238b08a6bad494e84ed1c632a62d39bdeed1f929950a05c1a32b6d4490a0047": {"wlid-01", "wlid-02"},
				"7238b08a6bad494e84ed1c632a62d39bdeed1f929950a05c1a32b6d4490a0048": {"wlid-03", "wlid-04"},
				"7238b08a6bad494e84ed1c632a62d39bdeed1f929950a05c1a32b6d4490a0049": {"wlid-05", "wlid-06"},
			},
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			iwMap := NewImageHashWLIDsMapFrom(tc.startingValues)

			iwAsMap := iwMap.Map()

			// Ensure consistent ordering of WLIDs
			for imageHash := range iwAsMap {
				sort.Strings(iwAsMap[imageHash])
			}
			assert.Equal(t, tc.startingValues, iwAsMap)
		})
	}
}
