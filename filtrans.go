//
// Copyright (c) 2010 - 2012 Yigong Liu
//
// Distributed under New BSD License
//

package router

/*
 IdFilter: the common interface of filters. concrete filters
 should be defined by apps with app-specific rules.
 if no filter defined, there is no id filtering.
 1. bound with specific proxy
 2. defines which ids can pass in / out to router thru this proxy
 3. only filter the ids of application msgs (NOT system msgs),
 only involved in processing namespace change msgs: PubId/SubId
 4. by default, if no filter is defined, everything is allowed
 5. filters are used against ids in local namespace, not translated ones
*/
type IdFilter interface {
	BlockInward(Id) bool
	BlockOutward(Id) bool
}

/*
 IdTransltor: the common interface of translators.
 concrete transltors should be defined by apps with app-specific rules.
 if no translator defined, there is no id transltions.
 1. bound with specific proxy
 2. translate ids of in / out msgs thru this proxy, effectively "mount" the msgs
 thru this proxy / conn to a subrange of router's id space
 3. only translate the ids of application msgs (NOT system msgs), and it will affect the
 ids of every app msgs passed thru this proxy - must be highly efficient
 4. by default, if no translator is defined, no translation
*/
type IdTranslator interface {
	TranslateInward(Id) Id
	TranslateOutward(Id) Id
}
