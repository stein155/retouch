// UI translations for the Radio app.
//
// LANGS is the picker list (each labelled with its own endonym). I18N holds the
// per-language string maps. makeT(lang) returns a t(key) lookup with a Dutch
// fallback. The chosen language is persisted per device (see /api/settings).
import { createContext, useContext } from 'react';

export const LANGS = [
  { code: 'en', label: 'English' },
  { code: 'de', label: 'Deutsch' },
  { code: 'nl', label: 'Nederlands' },
  { code: 'fr', label: 'Français' },
  { code: 'es', label: 'Español' },
  { code: 'af', label: 'Afrikaans' },
];

export const DEFAULT_LANG = 'nl';

export const I18N = {
  en: {
    appTitle: 'Radio', settings: 'Settings', thisRadio: 'This radio',
    name: 'Name', namePlaceholder: 'E.g. Living room',
    sound: 'Sound', bass: 'Bass', bassHint: 'Adjust the low end to taste.', language: 'Language',
    yourStations: 'Your stations', tapToPlay: 'tap to play',
    emptySlot: 'Empty slot', tapToFill: 'tap to fill', replace: 'Replace',
    findNew: 'Find something new', findNewSub: 'thousands of stations via TuneIn',
    connectedWith: 'connected to',
    chooseStation: 'Pick a station', discoverStations: 'Discover stations',
    forSlot: 'for slot', forSpeaker: 'for',
    searchPlaceholder: 'Stations, genres, countries…',
    searchPrompt: 'Search thousands of stations via TuneIn',
    noStations: 'No stations found.', searching: 'Searching…', clear: 'Clear', close: 'Close',
    live: 'LIVE', stop: 'Stop', mute: 'Mute', on: 'on', vol: 'vol',
    starting: 'Starting…', buffering: 'Buffering…',
    chooseStationFor: 'Pick a station for',
    software: 'Software', version: 'Version', updateNow: 'Update now',
    upToDate: 'You have the latest version',
    updating: 'Updating… the speaker restarts and comes back in about a minute.',
    updateDone: 'Updated to', updateError: 'Update failed. Please try again.',
    updatesOnSpeaker: 'Updates are only available on the speaker itself.',
  },
  nl: {
    appTitle: 'Radio', settings: 'Instellingen', thisRadio: 'Deze radio',
    name: 'Naam', namePlaceholder: 'Bijv. Woonkamer',
    sound: 'Geluid', bass: 'Bas', bassHint: 'Pas de lage tonen aan naar smaak.', language: 'Taal',
    yourStations: 'Jouw zenders', tapToPlay: 'tik om af te spelen',
    emptySlot: 'Lege plek', tapToFill: 'tik om in te vullen', replace: 'Vervangen',
    findNew: 'Vind iets nieuws', findNewSub: 'duizenden zenders via TuneIn',
    connectedWith: 'verbonden met',
    chooseStation: 'Kies een zender', discoverStations: 'Ontdek zenders',
    forSlot: 'voor plek', forSpeaker: 'voor',
    searchPlaceholder: 'Zenders, genres, landen…',
    searchPrompt: 'Doorzoek duizenden zenders via TuneIn',
    noStations: 'Geen zenders gevonden.', searching: 'Zoeken…', clear: 'Wissen', close: 'Sluiten',
    live: 'LIVE', stop: 'Stop', mute: 'Dempen', on: 'op', vol: 'vol',
    starting: 'Starten…', buffering: 'Bufferen…',
    chooseStationFor: 'Kies een zender voor',
    software: 'Software', version: 'Versie', updateNow: 'Nu bijwerken',
    upToDate: 'Je hebt de nieuwste versie',
    updating: 'Bezig met bijwerken… de speaker herstart en is over ongeveer een minuut terug.',
    updateDone: 'Bijgewerkt naar', updateError: 'Bijwerken mislukt. Probeer het opnieuw.',
    updatesOnSpeaker: 'Bijwerken kan alleen op de speaker zelf.',
  },
  de: {
    appTitle: 'Radio', settings: 'Einstellungen', thisRadio: 'Dieses Radio',
    name: 'Name', namePlaceholder: 'z. B. Wohnzimmer',
    sound: 'Klang', bass: 'Bass', bassHint: 'Passe die Tiefen nach Belieben an.', language: 'Sprache',
    yourStations: 'Deine Sender', tapToPlay: 'zum Abspielen tippen',
    emptySlot: 'Leerer Platz', tapToFill: 'zum Belegen tippen', replace: 'Ersetzen',
    findNew: 'Etwas Neues finden', findNewSub: 'Tausende Sender über TuneIn',
    connectedWith: 'verbunden mit',
    chooseStation: 'Sender wählen', discoverStations: 'Sender entdecken',
    forSlot: 'für Platz', forSpeaker: 'für',
    searchPlaceholder: 'Sender, Genres, Länder…',
    searchPrompt: 'Durchsuche Tausende Sender über TuneIn',
    noStations: 'Keine Sender gefunden.', searching: 'Suche…', clear: 'Löschen', close: 'Schließen',
    live: 'LIVE', stop: 'Stopp', mute: 'Stumm', on: 'auf', vol: 'Lautst.',
    starting: 'Startet…', buffering: 'Puffern…',
    chooseStationFor: 'Sender wählen für',
    software: 'Software', version: 'Version', updateNow: 'Jetzt aktualisieren',
    upToDate: 'Du hast die neueste Version',
    updating: 'Wird aktualisiert… der Lautsprecher startet neu und ist in etwa einer Minute zurück.',
    updateDone: 'Aktualisiert auf', updateError: 'Aktualisierung fehlgeschlagen. Bitte erneut versuchen.',
    updatesOnSpeaker: 'Aktualisierungen sind nur auf dem Lautsprecher selbst möglich.',
  },
  fr: {
    appTitle: 'Radio', settings: 'Réglages', thisRadio: 'Cette radio',
    name: 'Nom', namePlaceholder: 'Ex. Salon',
    sound: 'Son', bass: 'Basses', bassHint: 'Ajustez les basses à votre goût.', language: 'Langue',
    yourStations: 'Vos stations', tapToPlay: 'appuyez pour écouter',
    emptySlot: 'Emplacement libre', tapToFill: 'appuyez pour remplir', replace: 'Remplacer',
    findNew: 'Trouver du nouveau', findNewSub: 'des milliers de stations via TuneIn',
    connectedWith: 'connecté à',
    chooseStation: 'Choisir une station', discoverStations: 'Découvrir des stations',
    forSlot: "pour l'emplacement", forSpeaker: 'pour',
    searchPlaceholder: 'Stations, genres, pays…',
    searchPrompt: 'Cherchez des milliers de stations via TuneIn',
    noStations: 'Aucune station trouvée.', searching: 'Recherche…', clear: 'Effacer', close: 'Fermer',
    live: 'DIRECT', stop: 'Stop', mute: 'Muet', on: 'sur', vol: 'vol.',
    starting: 'Démarrage…', buffering: 'Chargement…',
    chooseStationFor: 'Choisir une station pour',
    software: 'Logiciel', version: 'Version', updateNow: 'Mettre à jour',
    upToDate: 'Vous avez la dernière version',
    updating: "Mise à jour… l'enceinte redémarre et revient dans environ une minute.",
    updateDone: 'Mis à jour vers', updateError: 'Échec de la mise à jour. Réessayez.',
    updatesOnSpeaker: "Les mises à jour ne sont disponibles que sur l'enceinte elle-même.",
  },
  es: {
    appTitle: 'Radio', settings: 'Ajustes', thisRadio: 'Esta radio',
    name: 'Nombre', namePlaceholder: 'Ej. Salón',
    sound: 'Sonido', bass: 'Graves', bassHint: 'Ajusta los graves a tu gusto.', language: 'Idioma',
    yourStations: 'Tus emisoras', tapToPlay: 'toca para reproducir',
    emptySlot: 'Espacio vacío', tapToFill: 'toca para rellenar', replace: 'Reemplazar',
    findNew: 'Descubre algo nuevo', findNewSub: 'miles de emisoras vía TuneIn',
    connectedWith: 'conectado a',
    chooseStation: 'Elige una emisora', discoverStations: 'Descubre emisoras',
    forSlot: 'para el espacio', forSpeaker: 'para',
    searchPlaceholder: 'Emisoras, géneros, países…',
    searchPrompt: 'Busca miles de emisoras vía TuneIn',
    noStations: 'No se encontraron emisoras.', searching: 'Buscando…', clear: 'Borrar', close: 'Cerrar',
    live: 'EN VIVO', stop: 'Detener', mute: 'Silenciar', on: 'en', vol: 'vol.',
    starting: 'Iniciando…', buffering: 'Cargando…',
    chooseStationFor: 'Elige una emisora para',
    software: 'Software', version: 'Versión', updateNow: 'Actualizar ahora',
    upToDate: 'Tienes la última versión',
    updating: 'Actualizando… el altavoz se reinicia y vuelve en aproximadamente un minuto.',
    updateDone: 'Actualizado a', updateError: 'Error al actualizar. Inténtalo de nuevo.',
    updatesOnSpeaker: 'Las actualizaciones solo están disponibles en el altavoz.',
  },
  af: {
    appTitle: 'Radio', settings: 'Instellings', thisRadio: 'Hierdie radio',
    name: 'Naam', namePlaceholder: 'Bv. Sitkamer',
    sound: 'Klank', bass: 'Bas', bassHint: 'Pas die lae tone na smaak aan.', language: 'Taal',
    yourStations: 'Jou stasies', tapToPlay: 'tik om te speel',
    emptySlot: 'Leë plek', tapToFill: 'tik om in te vul', replace: 'Vervang',
    findNew: 'Vind iets nuuts', findNewSub: 'duisende stasies via TuneIn',
    connectedWith: 'gekoppel aan',
    chooseStation: "Kies 'n stasie", discoverStations: 'Ontdek stasies',
    forSlot: 'vir plek', forSpeaker: 'vir',
    searchPlaceholder: 'Stasies, genres, lande…',
    searchPrompt: 'Deursoek duisende stasies via TuneIn',
    noStations: 'Geen stasies gevind nie.', searching: 'Soek…', clear: 'Vee uit', close: 'Maak toe',
    live: 'LIVE', stop: 'Stop', mute: 'Doof', on: 'op', vol: 'vol',
    starting: 'Begin…', buffering: 'Buffer…',
    chooseStationFor: "Kies 'n stasie vir",
    software: 'Sagteware', version: 'Weergawe', updateNow: 'Werk nou by',
    upToDate: 'Jy het die nuutste weergawe',
    updating: "Besig om by te werk… die luidspreker herbegin en is oor ongeveer 'n minuut terug.",
    updateDone: 'Bygewerk na', updateError: 'Bywerk het misluk. Probeer weer.',
    updatesOnSpeaker: 'Bywerkings is net op die luidspreker self beskikbaar.',
  },
};

export function isSupportedLang(code) {
  return !!I18N[code];
}

// Initial language: the browser's preferred language if supported, else Dutch.
export function detectInitialLang() {
  const candidates = (typeof navigator !== 'undefined' && navigator.languages?.length)
    ? navigator.languages
    : [typeof navigator !== 'undefined' ? navigator.language : ''];
  for (const c of candidates) {
    const base = String(c || '').toLowerCase().split('-')[0];
    if (isSupportedLang(base)) return base;
  }
  return DEFAULT_LANG;
}

// Build a translate function for a language, falling back to Dutch then the key.
export function makeT(lang) {
  const dict = I18N[lang] || I18N[DEFAULT_LANG];
  const fb = I18N[DEFAULT_LANG];
  return (key) => (dict[key] != null ? dict[key] : (fb[key] != null ? fb[key] : key));
}

export const I18nContext = createContext({ t: (k) => k, lang: DEFAULT_LANG });
export const useI18n = () => useContext(I18nContext);
